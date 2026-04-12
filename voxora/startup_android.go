//go:build android
// +build android

package main

import "C"

//export AndroidMain
func AndroidMain() {
	app_main()
}

func main() {
	// Must be empty.
}
