package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"reflect"
	"sync"
	"time"

	"github.com/daedaleanai/ublox"
	"github.com/daedaleanai/ublox/ubx"
	"github.com/tarm/serial"
)

func main() {

	config := &serial.Config{
		Name:     "/dev/ttyAMA1", //todo: make this configurable
		Baud:     38400,
		Parity:   serial.ParityNone,
		StopBits: serial.Stop1,
	}

	stream, err := serial.OpenPort(config)
	handleError("opening gps serial port", err)

	timeSet := make(chan time.Time)
	timeGetter := NewTimeGetter(timeSet)

	messageHandlersLock := sync.Mutex{}
	messageHandlers := map[reflect.Type][]messageHandler{}
	messageHandlers[reflect.TypeOf(&ubx.NavPvt{})] = []messageHandler{timeGetter}

	fmt.Println("handlers", messageHandlers)
	go func() {
		d := ublox.NewDecoder(stream)
		for {
			msg, err := d.Decode()
			if err != nil {
				if err == io.EOF {
					break
				}
				if err.Error() == "invalid UBX checksum" {
					fmt.Println("WARNING: invalid UBX checksum")
					continue
				}
				handleError("decoding ubx", err)
			}
			//fmt.Println("received message", msg, "of type", reflect.TypeOf(msg))
			messageHandlersLock.Lock()
			handlers := messageHandlers[reflect.TypeOf(msg)]
			for _, handler := range handlers {
				handler.handle(msg)
			}
			messageHandlersLock.Unlock()
		}
	}()

	now := time.Time{}
	loadAll := false
	select {
	case now = <-timeSet:
		messageHandlersLock.Lock()
		delete(messageHandlers, reflect.TypeOf(&ubx.NavPvt{})) //todo: this is a hack, we should have a way to unregister handlers
		messageHandlersLock.Unlock()

	case <-time.After(5 * time.Second):
		fmt.Println("not time yet, will load all ano messages")
		loadAll = true
	}

	mgaOfflineFilePath := os.Args[1]
	if _, err := os.Stat(mgaOfflineFilePath); errors.Is(err, os.ErrNotExist) {
		fmt.Printf("File %s does not exist\n", mgaOfflineFilePath)
	} else {
		loader := NewAnoLoader()
		messageHandlers[reflect.TypeOf(&ubx.MgaAckData0{})] = []messageHandler{loader}
		err = loader.loadAnoFile(mgaOfflineFilePath, loadAll, now, stream)
		handleError("loading ano file", err)
	}

	fmt.Println()
	if now == (time.Time{}) {
		fmt.Println("Waiting for time")
		now = <-timeSet
	}
}

type messageHandler interface {
	handle(interface{})
}

type AnoLoader struct {
	anoPerSatellite map[uint8]int
	ackChannel      chan *ubx.MgaAckData0
}

func NewAnoLoader() *AnoLoader {
	return &AnoLoader{
		anoPerSatellite: map[uint8]int{},
		ackChannel:      make(chan *ubx.MgaAckData0),
	}
}

func (l *AnoLoader) loadAnoFile(file string, loadAll bool, now time.Time, stream io.Writer) error {
	fmt.Println("loading mga offline file:", file)
	mgaOfflineFile, err := os.Open(file)
	handleError("opening mga file", err)

	mgaOfflineDecoder := ublox.NewDecoder(mgaOfflineFile)
	sentCount := 0
	for {
		msg, err := mgaOfflineDecoder.Decode()
		if err != nil {
			if err == io.EOF {
				fmt.Println("reach mga EOF")
				break
			}
			handleError("decoding ubx from mga offline file", err)
		}
		ano := msg.(*ubx.MgaAno)
		anoDate := time.Date(int(ano.Year)+2000, time.Month(ano.Month), int(ano.Day), 0, 0, 0, 0, time.UTC)
		if loadAll || (anoDate.Year() == now.Year() && anoDate.Month() == now.Month() && anoDate.Day() == now.Day()) { //todo: get system date
			fmt.Println("processing an ANO message")
			encoded, err := ubx.Encode(msg.(ubx.Message))
			if err != nil {
				return fmt.Errorf("encoding ano message: %w", err)
			}
			_, err = stream.Write(encoded)
			if err != nil {
				return fmt.Errorf("writing to stream: %w", err)
			}
			time.Sleep(100 * time.Millisecond)

		goAck:
			for {
				select {
				case ack := <-l.ackChannel:
					d, err := json.Marshal(ack)
					if err != nil {
						return err
					}
					fmt.Println("got ack:", string(d))
					break goAck
				case <-time.After(5 * time.Second):
					return errors.New("timeout waiting for ack")
				}
			}
			fmt.Print(".")
			sentCount++
		}
	}

	return nil
}

func (l *AnoLoader) handle(message interface{}) {
	fmt.Println("handling ack")
	ack := message.(*ubx.MgaAckData0)
	l.ackChannel <- ack
}

type TimeGetter struct {
	done chan time.Time
}

func NewTimeGetter(done chan time.Time) *TimeGetter {
	return &TimeGetter{done: done}
}

func (g *TimeGetter) handle(message interface{}) {
	navPvt := message.(*ubx.NavPvt)
	fmt.Println("time getter nav pvt info, date validity:", navPvt.Valid, "accuracy:", navPvt.TAcc_ns, "lock type:", navPvt.FixType, "flags:", navPvt.Flags, "flags2:", navPvt.Flags2, "flags3:", navPvt.Flags3)
	if navPvt.Valid&0x1 == 0 {
		return
	}
	now := time.Date(int(navPvt.Year_y), time.Month(int(navPvt.Month_month)), int(navPvt.Day_d), int(navPvt.Hour_h), int(navPvt.Min_min), int(navPvt.Sec_s), int(navPvt.Nano_ns), time.UTC)
	fmt.Println("Got a valid date:", now)

	err := SetSystemDate(now)
	if err != nil {
		fmt.Println("Error setting system date:", err)
		os.Exit(1)
	}
	g.done <- now
}

func SetSystemDate(newTime time.Time) error {
	_, err := exec.LookPath("date")
	if err != nil {
		return fmt.Errorf("look for date binary: %w", err)
	} else {
		dateString := newTime.Format("2006-01-02 15:04:05")
		//dateString := newTime.Format("2 Jan 2006 15:04:05")
		fmt.Printf("Setting system date to: %s\n", dateString)
		args := []string{"--set", dateString}
		cmd := exec.Command("date", args...)
		fmt.Println("Running cmd:", cmd.String())
		return cmd.Run()
	}
}

func handleError(context string, err error) {
	if err != nil {
		log.Fatalln(fmt.Sprintf("%s: %s\n", context, err.Error()))
	}
}
