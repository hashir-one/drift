package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"image/color"
)

var (
	ink     = color.NRGBA{R: 27, G: 35, B: 52, A: 255}
	muted   = color.NRGBA{R: 105, G: 117, B: 139, A: 255}
	accent  = color.NRGBA{R: 94, G: 81, B: 223, A: 255}
	surface = color.NRGBA{R: 246, G: 247, B: 251, A: 255}
	border  = color.NRGBA{R: 227, G: 231, B: 239, A: 255}
	navy    = color.NRGBA{R: 21, G: 29, B: 47, A: 255}
)

type studioTheme struct{}

func (studioTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return surface
	case theme.ColorNameForeground:
		return ink
	case theme.ColorNamePrimary, theme.ColorNameHyperlink:
		return accent
	case theme.ColorNameButton, theme.ColorNameInputBackground, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return color.White
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return muted
	case theme.ColorNameSeparator, theme.ColorNameInputBorder:
		return border
	case theme.ColorNameHover:
		return color.NRGBA{R: 94, G: 81, B: 223, A: 15}
	case theme.ColorNameSelection, theme.ColorNameFocus:
		return color.NRGBA{R: 94, G: 81, B: 223, A: 35}
	}
	return theme.DefaultTheme().Color(n, theme.VariantLight)
}
func (studioTheme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (studioTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (studioTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNamePadding:
		return 10
	case theme.SizeNameInnerPadding:
		return 12
	case theme.SizeNameText:
		return 14
	case theme.SizeNameInputRadius:
		return 8
	case theme.SizeNameSelectionRadius:
		return 8
	}
	return theme.DefaultTheme().Size(n)
}
func txt(s string, size float32, bold bool, c color.Color) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size
	t.TextStyle = fyne.TextStyle{Bold: bold}
	return t
}
func sub(s string) *widget.Label { l := widget.NewLabel(s); l.Wrapping = fyne.TextWrapWord; return l }
func ellipsis(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Truncation = fyne.TextTruncateEllipsis
	return l
}
func gap(h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(1, h))
	return r
}
func inset(n float32, o fyne.CanvasObject) *fyne.Container {
	return container.New(layout.NewCustomPaddedLayout(n, n, n, n), o)
}
func panel(o fyne.CanvasObject) *fyne.Container {
	r := canvas.NewRectangle(color.White)
	r.CornerRadius = 12
	r.StrokeColor = border
	r.StrokeWidth = 1
	return container.NewStack(r, inset(20, o))
}
func section(title, description string) *fyne.Container {
	return container.NewVBox(txt(title, 18, true, ink), txt(description, 12, false, muted))
}

type fileRow struct {
	widget.BaseWidget
	name, path, state *widget.Label
	icon              *widget.Icon
}

func newFileRow() *fileRow {
	r := &fileRow{name: ellipsis(""), path: ellipsis(""), state: widget.NewLabel("Ready"), icon: widget.NewIcon(theme.FileVideoIcon())}
	r.name.TextStyle.Bold = true
	r.path.TextStyle = fyne.TextStyle{}
	r.ExtendBaseWidget(r)
	return r
}
func (r *fileRow) CreateRenderer() fyne.WidgetRenderer {
	info := container.NewVBox(r.name, r.path)
	iconBox := container.NewGridWrap(fyne.NewSize(34, 34), r.icon)
	return widget.NewSimpleRenderer(container.NewBorder(nil, nil, container.NewCenter(iconBox), container.NewCenter(r.state), info))
}
func (r *fileRow) Set(e queueEntry) {
	r.name.SetText(fileName(e.Path))
	r.path.SetText(e.Path)
	r.state.SetText(e.State)
	if e.State == "Converted" {
		r.icon.SetResource(theme.ConfirmIcon())
	} else if e.State == "Failed" {
		r.icon.SetResource(theme.ErrorIcon())
	} else {
		r.icon.SetResource(theme.FileVideoIcon())
	}
}
