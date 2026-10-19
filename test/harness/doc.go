// Package harness is the hermetic test harness of SCOPE.md §18.1. It holds
// deterministic fakes of the operating system seams so failure paths run on
// every OS in CI: a fake clock, throw-away Git repositories isolated from the
// developer's configuration, fault injection into the Git subprocess (exit
// code, timeout, truncated output), a fake Host and a simulated VHDX-backed
// Dev Drive.
//
// Tests built on it never touch the wall clock, the network, the real
// machine or the developer's Git config.
package harness
