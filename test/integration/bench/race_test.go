//go:build race

package bench

// raceEnabled is true when the race detector slows every operation several
// times over, which makes absolute timings meaningless.
const raceEnabled = true
