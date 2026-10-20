// Package benchrepo generates deterministic benchmark repositories offline
// (SCOPE.md §18.1, §20.21). The same profile and seed produce the same Git
// trees on every machine, so benchmark numbers compare across machines and
// runs. Repositories contain only Windows-safe paths and incompressible file
// contents, and are written with one git fast-import process.
package benchrepo
