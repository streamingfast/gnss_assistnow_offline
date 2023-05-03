package main

import (
	"fmt"
	"github.com/daedaleanai/ublox"
	"github.com/daedaleanai/ublox/ubx"
	"github.com/tarm/serial"
	"io"
	"log"
	"time"
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
	fmt.Println("Opened serial port")
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

	for {
		msg := <-messageChannel
		switch m := msg.(type) {
		case *ubx.NavPvt:
			d := time.Date(int(m.Year_y), time.Month(int(m.Month_month)), int(m.Day_d), int(m.Hour_h), int(m.Min_min), int(m.Sec_s), int(m.Nano_ns), time.UTC)
			fmt.Println("Time:", d)
		default:
			fmt.Printf("Received unknown message type %T\n", msg)
		}
	}

}

func handleError(context string, err error) {
	if err != nil {
		log.Fatalln(fmt.Sprintf("%s: %s\n", context, err.Error()))
	}
}
