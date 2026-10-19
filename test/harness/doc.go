// Package harness is the hermetic test harness of SCOPE.md §18.1. It holds
// deterministic fakes of the operating system seams so failure paths run on
// every OS in CI: a fake Host and a simulated VHDX-backed Dev Drive.
//
// Nothing here touches the real machine, spawns goroutines or reads the
// clock.
package harness
