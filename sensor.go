package main

import "strings"

// zoneTriggered interprets the raw GPIO state of a zone according to its mode.
// For normally closed (NC) circuits, a low signal (false) indicates that the
// sensor has been tripped (circuit broken).  For normally open (NO) circuits,
// a high signal (true) indicates activation.  End-of-line (EOL) circuits
// typically use resistive dividers to detect tamper; our stub treats them
// like normally open sensors.  Any unrecognised mode defaults to NO semantics.
func zoneTriggered(z Zone) bool {
    state := readPin(z.Pin)
    switch strings.ToUpper(z.Mode) {
    case "NC":
        // Normally closed: low means triggered
        return !state
    case "NO":
        // Normally open: high means triggered
        return state
    case "EOL":
        // End-of-line: treat high as triggered for this sample implementation
        return state
    default:
        return state
    }
}