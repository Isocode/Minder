package main

// This file defines a simple hardware abstraction layer (HAL) for GPIO access.
// It is intentionally minimal: in the default build it returns fixed values
// so that you can run and test the web server on a desktop machine without
// Raspberry Pi hardware.  To use real GPIO on the Pi, implement a separate
// file (e.g. hal_rpi.go) with the same functions, guarded by a build tag.

// readPin returns the logic level of the given GPIO pin.  In the stub
// implementation it always returns false (no trigger).  On the Pi you would
// call into go-rpio or another GPIO library to read the pin state.
func readPin(pin int) bool {
    // TODO: replace with real GPIO access when building on the Pi
    return false
}

// initGPIO performs any global initialisation required to access GPIO pins.
// In the stub implementation it does nothing.  On the Pi this might open
// /dev/mem or load kernel modules.
func initGPIO() error {
    return nil
}