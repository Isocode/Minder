//go:build linux && arm && !disablegpio
// +build linux,arm,!disablegpio

// This file provides a Raspberry Pi implementation of the HAL functions using
// the periph.io library.  When cross‑compiling on other platforms or when
// the build tag "disablegpio" is specified, hal.go will be used instead.

package main

import (
    "fmt"
    // Use the new periph module layout.  See https://periph.io/news/2020/a_new_start/
    "periph.io/x/conn/v3/gpio"
    "periph.io/x/conn/v3/gpio/gpioreg"
    "periph.io/x/host/v3"
)

// readPin reads the specified GPIO pin and returns true if the voltage level
// is high.  If the GPIO cannot be initialised or the pin name is invalid,
// it returns false.  Pins are addressed by their BCM numbers.
func readPin(pin int) bool {
    // Ensure the host is initialised.  host.Init can safely be called multiple
    // times; subsequent calls will be no‑ops.
    _, err := host.Init()
    if err != nil {
        return false
    }
    p := gpioreg.ByName(fmt.Sprintf("GPIO%d", pin))
    if p == nil {
        return false
    }
    return p.Read() == gpio.High
}

// initGPIO initialises periph host state.  Returning an error here will
// prevent the server from starting.  This function is called once during
// server startup.
func initGPIO() error {
    _, err := host.Init()
    return err
}