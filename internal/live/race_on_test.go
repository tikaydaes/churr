//go:build race

package live

// raceDetector says the tests were built with -race, under which a
// goroutine and its stack are several times their real size.
const raceDetector = true
