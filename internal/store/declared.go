package store

// The areas in this file are declared so that Areas is complete and nobody
// has to edit store.go to start one. Each has an empty implementation in
// sqlstore and an empty test function in storetest.
//
// To build one: move its interface into a file of its own name here, give it
// methods, implement them in the sqlstore file of the same name, and write
// its storage tests in the storetest file of the same name. README.md has
// the steps.
