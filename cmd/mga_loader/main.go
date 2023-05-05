package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
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

	messageChannel := make(chan interface{})
	go func() {
		d := ublox.NewDecoder(stream)
		for {
			msg, err := d.Decode()
			if err != nil {
				if err == io.EOF {
					break
				}
				handleError("decoding ubx", err)
			}
			messageChannel <- msg
		}
	}()

	fmt.Println("Waiting for time")
	timeWaitStart := time.Now()
	now := time.Now()
gotTime:
	for {
		msg := <-messageChannel
		switch m := msg.(type) {
		case *ubx.NavPvt:
			fmt.Println("NavPvt info, date validity:", m.Valid, "accuracy:", m.TAcc_ns, "lock type:", m.FixType, "flags:", m.Flags, "flags2:", m.Flags2, "flags3:", m.Flags3)
			now = time.Date(int(m.Year_y), time.Month(int(m.Month_month)), int(m.Day_d), int(m.Hour_h), int(m.Min_min), int(m.Sec_s), int(m.Nano_ns), time.UTC)
			if m.Valid&0x1 != 0 {
				fmt.Println("Got a valid date:", now)
				break gotTime
			}
		default:
		}
	}
	//todo: set system time
	fmt.Println("time set:", time.Since(timeWaitStart))

	mgaOfflineFilePath := os.Args[1]
	if _, err := os.Stat(mgaOfflineFilePath); errors.Is(err, os.ErrNotExist) {
		fmt.Printf("File %s does not exist\n", mgaOfflineFilePath)
		os.Exit(1)
	}

	mgaOfflineFile, err := os.Open(mgaOfflineFilePath)
	handleError("opening mga file", err)

	start := time.Now()
	mgaOfflineDecoder := ublox.NewDecoder(mgaOfflineFile)
	for {
		msg, err := mgaOfflineDecoder.Decode()
		if err != nil {
			if err == io.EOF {
				break
			}
			handleError("decoding ubx from mga offline file", err)
		}
		ano := msg.(*ubx.MgaAno)
		anoDate := time.Date(int(ano.Year)+2000, time.Month(ano.Month), int(ano.Day), 0, 0, 0, 0, time.UTC)

		if anoDate.Year() == now.Year() && anoDate.Month() == now.Month() && anoDate.Day() == now.Day() { //todo: get system date
			encoded, err := ubx.Encode(msg.(ubx.Message))
			handleError("encoding ubx", err)
			_, err = stream.Write(encoded)
			handleError("writing to gpsd", err)
			fmt.Printf("Sent: %#v\n", msg)

			var ack *ubx.MgaAckData0
			for {
				fmt.Println("Waiting for ack")
				select {
				case msg := <-messageChannel:
					if a, ok := msg.(*ubx.MgaAckData0); ok {
						ack = a
						fmt.Println("Got ack:", ack)
					}
				case <-time.After(1 * time.Second):
					panic("Timed out")
				}
				if ack != nil {
					break
				}
			}
		}
	}
	fmt.Println("Send all ubx.MgaAno messageChannel", time.Since(start))
}

func handleError(context string, err error) {
	if err != nil {
		log.Fatalln(fmt.Sprintf("%s: %s\n", context, err.Error()))
	}
}
