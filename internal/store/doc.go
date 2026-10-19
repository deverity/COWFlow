// Package store owns the SQLite metadata database (SCOPE.md §13): opening it
// in WAL mode with a busy timeout, applying embedded migrations, and the
// transaction helpers built on top. Git stays canonical for code; SQLite holds
// metadata, events and derived indexes.
package store
