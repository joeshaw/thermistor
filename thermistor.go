package main

import (
	"context"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/brutella/hc"
	"github.com/brutella/hc/accessory"
	"github.com/brutella/hc/service"

	"gobot.io/x/gobot/drivers/i2c"
	"gobot.io/x/gobot/platforms/raspi"
)

// From the Happy Feet datasheet:
//
// ohms    degC
// 64000   -10
// 38000     0
// 23300    10
// 14800    20
//  9700    30

const (
	// Voltage in
	vin = 3.3

	// Fixed resistor, in ohms
	rfixed = 10000

	// Reference values from the Happy Feet datasheet.  Resistance
	// of 64000 ohms at -10C.
	refr = 64000
	reft = -10

	// Thermistor beta coefficient, used in Steinhart–Hart
	// equation.  This is derived from the values in the
	// datasheet:
	//
	// ln(r1/r2) / (1 / (273.15+t1) - 1 / (273.15+t2))
	//
	// Where r1 and r2 are resistance values and t1 and t2 are
	// temperature values.  Taking the average of all of the
	// possible values from the refernece values gives us this
	// beta value.
	beta = 3765
)

func main() {
	board := raspi.NewAdaptor()
	ads1015 := i2c.NewADS1015Driver(board)

	if err := ads1015.Start(); err != nil {
		log.Fatal(err)
	}

	info := accessory.Info{
		Name:         "Heated floor",
		Manufacturer: "Happy Feet",
	}

	acc := accessory.New(info, accessory.TypeThermostat)
	sensors := make([]*service.TemperatureSensor, 2)
	for i := 0; i < 2; i++ {
		sensors[i] = service.NewTemperatureSensor()
		sensors[i].CurrentTemperature.SetMinValue(-10)
		sensors[i].CurrentTemperature.SetMaxValue(50)
		acc.AddService(sensors[i].Service)
	}

	cfg := hc.Config{
		Pin:         "00102003",
		StoragePath: filepath.Join(os.Getenv("HOME"), ".homecontrol", "thermistors"),
	}

	ipt, err := hc.NewIPTransport(cfg, acc)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hc.OnTermination(func() {
		cancel()
		<-ipt.Stop()
	})

	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-t.C:
				for i := 0; i < 2; i++ {
					const nSamples = 10

					var total float64
					for j := 0; j < nSamples; j++ {
						v, err := ads1015.ReadWithDefaults(i)
						if err != nil {
							log.Fatal(err)
						}
						total += v

						time.Sleep(100 * time.Millisecond)
					}
					v := total / nSamples

					log.Printf("A%d voltage: %f", i, v)

					// Use our fixed 10 kohm
					// resistor to convert voltage
					// to resistance.
					r := (v * rfixed) / (vin - v)
					log.Printf("A%d resistance: %f", i, r)

					// Steinhart-Hart equation
					tc := 1/(math.Log(r/refr)/beta+1/(reft+273.15)) - 273.15

					// Fahrenheit for display
					tf := tc*9/5 + 32
					log.Printf("A%d temperature: %f", i, tf)

					sensors[i].CurrentTemperature.SetValue(tc)
				}
			}
		}
	}()

	log.Println("Starting transport...")
	ipt.Start()
}
