package main

import (
	_ "embed"
	"fyne.io/fyne/v2"
)

//go:embed assets/drift-icon.svg
var driftIconSVG []byte

var driftIcon = fyne.NewStaticResource("drift-icon.svg", driftIconSVG)

const appName = "Drift"
const appVersion = "2.1"
