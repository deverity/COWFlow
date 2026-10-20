// Package bench measures the Sprint 1 exit criteria on a real copy-on-write
// volume (SCOPE.md §19 Phase 1 output, §20.17, §20.21, §23 decision 43): the
// time of Manager.Create, the incremental disk per workspace, the gain of the
// shared Go caches and the cost of a golden tree per base commit.
//
// The package holds only tests and the result writer. Every benchmark skips
// with a reason unless COWFLOW_BENCH_ROOT, COWFLOW_TEST_COW_DIR or
// COWFLOW_TEST_REFS_DIR names a directory on a volume with block clone, so
// the package is safe in the default test run. Each test writes one JSON
// document to COWFLOW_BENCH_OUT when that is set.
package bench
