//go:build windows
// +build windows

package main

import (
	"fmt"
	"math"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/faiface/pixel"
	"github.com/faiface/pixel/imdraw"
	"github.com/faiface/pixel/pixelgl"
	"github.com/faiface/pixel/text"
	"github.com/go-vgo/robotgo"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/sys/windows"
)

const (
	PROCESS_PER_MONITOR_DPI_AWARE = 2
	PROCESS_SYSTEM_DPI_AWARE      = 1
	MDT_EFFECTIVE_DPI             = 0
	MDT_ANGULAR_DPI               = 1
	MDT_RAW_DPI                   = 2
	SWP_NOMOVE                    = 0x0002
	SWP_NOSIZE                    = 0x0001
	HWND_TOPMOST                  = -1
	HWND_NOTOPMOST                = -2
	CLICK_DELAY                   = 250 * time.Millisecond
	UPDATE_DELAY                  = 10 * time.Millisecond
)

var (
	shcore                 = syscall.NewLazyDLL("Shcore.dll")
	setProcessDpiAwareness = shcore.NewProc("SetProcessDpiAwareness")
	getDpiForMonitor       = shcore.NewProc("GetDpiForMonitor")
	user32                 = windows.NewLazyDLL("user32.dll")
	gdi32                  = windows.NewLazyDLL("gdi32.dll")
	getDC                  = user32.NewProc("GetDC")
	releaseDC              = user32.NewProc("ReleaseDC")
	getPixel               = gdi32.NewProc("GetPixel")
	windowFromPoint        = user32.NewProc("WindowFromPoint")
	logicalToPhysicalPoint = user32.NewProc("LogicalToPhysicalPointForPerMonitorDPI")
	setProcessDPIAware     = user32.NewProc("SetProcessDPIAware")
	setWindowPos           = user32.NewProc("SetWindowPos")
	findWindow             = user32.NewProc("FindWindowW")
	isPaused               bool
	lastX                  int
	lastY                  int
	colorInfo              ColorInfo
	rInt, gInt, bInt       uint8
	isAlwaysOnTop          bool = true
	windowHandle           uintptr
)

type POINT struct {
	X, Y int32
}

type ColorInfo struct {
	RGB  string
	RGBA string
	HEX  string
	HSL  string
	CMYK string
	HSV  string
}

type ColorBox struct {
	bounds      pixel.Rect
	label       string
	value       string
	valueBounds pixel.Rect
}

type Checkbox struct {
	bounds  pixel.Rect
	checked bool
	label   string
}

func init() {
	if runtime.GOOS == "windows" {
		setProcessDPIAware.Call()
		setProcessDpiAwareness.Call(uintptr(PROCESS_PER_MONITOR_DPI_AWARE))
	}
}

func getAccuratePixelColor(x, y int) (uint8, uint8, uint8) {
	if runtime.GOOS == "windows" {
		pt := POINT{X: int32(x), Y: int32(y)}
		hwnd, _, _ := windowFromPoint.Call(uintptr(unsafe.Pointer(&pt)))

		monitor, _, _ := user32.NewProc("MonitorFromPoint").Call(
			uintptr(unsafe.Pointer(&pt)),
			uintptr(0x00000002),
		)

		var dpiX, dpiY uint32
		getDpiForMonitor.Call(
			monitor,
			uintptr(MDT_EFFECTIVE_DPI),
			uintptr(unsafe.Pointer(&dpiX)),
			uintptr(unsafe.Pointer(&dpiY)),
		)

		dpiScale := float64(dpiX) / 96.0

		logicalToPhysicalPoint.Call(hwnd, uintptr(unsafe.Pointer(&pt)))

		scaledX := int(float64(pt.X) / dpiScale)
		scaledY := int(float64(pt.Y) / dpiScale)

		hdc, _, _ := getDC.Call(0)
		if hdc == 0 {
			return 0, 0, 0
		}
		defer releaseDC.Call(0, hdc)

		colorRef, _, _ := getPixel.Call(hdc, uintptr(scaledX), uintptr(scaledY))
		r := uint8(colorRef & 0xFF)
		g := uint8((colorRef >> 8) & 0xFF)
		b := uint8((colorRef >> 16) & 0xFF)
		return r, g, b
	} else {
		hexColor := robotgo.GetPixelColor(x, y)
		rStr := hexColor[0:2]
		gStr := hexColor[2:4]
		bStr := hexColor[4:6]

		var rInt, gInt, bInt uint8
		fmt.Sscanf(rStr, "%x", &rInt)
		fmt.Sscanf(gStr, "%x", &gInt)
		fmt.Sscanf(bStr, "%x", &bInt)

		return rInt, gInt, bInt
	}
}

func rgbToHex(r, g, b uint8) string {
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

func rgbToHSL(r, g, b uint8) (h, s, l float64) {
	rr := float64(r) / 255.0
	gg := float64(g) / 255.0
	bb := float64(b) / 255.0

	max := math.Max(rr, math.Max(gg, bb))
	min := math.Min(rr, math.Min(gg, bb))
	delta := max - min

	l = (max + min) / 2.0

	if delta == 0 {
		return 0, 0, l
	}

	s = delta / (2.0 - max - min)
	if l < 0.5 {
		s = delta / (max + min)
	}

	switch {
	case rr == max:
		h = (gg - bb) / delta
		if gg < bb {
			h += 6
		}
	case gg == max:
		h = 2.0 + (bb-rr)/delta
	default:
		h = 4.0 + (rr-gg)/delta
	}

	return h * 60, s * 100, l * 100
}

func rgbToCMYK(r, g, b uint8) (c, m, y, k float64) {
	if r == 0 && g == 0 && b == 0 {
		return 0, 0, 0, 100
	}

	rr := float64(r) / 255.0
	gg := float64(g) / 255.0
	bb := float64(b) / 255.0

	k = 1.0 - math.Max(rr, math.Max(gg, bb))
	invK := 1.0 - k
	if invK != 0 {
		c = (1.0 - rr - k) / invK * 100
		m = (1.0 - gg - k) / invK * 100
		y = (1.0 - bb - k) / invK * 100
	}
	k *= 100

	return
}

func rgbToHSV(r, g, b uint8) (h, s, v float64) {
	rr := float64(r) / 255.0
	gg := float64(g) / 255.0
	bb := float64(b) / 255.0

	max := math.Max(rr, math.Max(gg, bb))
	min := math.Min(rr, math.Min(gg, bb))
	delta := max - min

	v = max * 100

	if max == 0 {
		return 0, 0, v
	}

	s = (delta / max) * 100

	if delta == 0 {
		return 0, s, v
	}

	switch {
	case rr == max:
		h = (gg - bb) / delta
		if gg < bb {
			h += 6
		}
	case gg == max:
		h = 2.0 + (bb-rr)/delta
	default:
		h = 4.0 + (rr-gg)/delta
	}

	return h * 60, s, v
}

func getColorInfo(r, g, b uint8) ColorInfo {
	h, s, l := rgbToHSL(r, g, b)
	c, m, y, k := rgbToCMYK(r, g, b)
	hv, sv, vv := rgbToHSV(r, g, b)

	return ColorInfo{
		RGB:  fmt.Sprintf("%d, %d, %d", r, g, b),
		RGBA: fmt.Sprintf("%d, %d, %d, 1", r, g, b),
		HEX:  rgbToHex(r, g, b),
		HSL:  fmt.Sprintf("%.1f, %.1f%%, %.1f%%", h, s, l),
		CMYK: fmt.Sprintf("%.1f%%, %.1f%%, %.1f%%, %.1f%%", c, m, y, k),
		HSV:  fmt.Sprintf("%.1f, %.1f%%, %.1f%%", hv, sv, vv),
	}
}

func setTopMost(top bool) bool {
	if runtime.GOOS != "windows" {
		return false
	}

	if windowHandle == 0 {
		return false
	}

	var flag int = HWND_NOTOPMOST
	if top {
		flag = HWND_TOPMOST
	}

	ret, _, _ := setWindowPos.Call(
		windowHandle,
		uintptr(flag),
		0, 0, 0, 0,
		uintptr(SWP_NOMOVE|SWP_NOSIZE),
	)

	return ret != 0
}

func run() {
	cfg := pixelgl.WindowConfig{
		Title:       "colorex",
		Bounds:      pixel.R(0, 0, 315, 240),
		VSync:       true,
		AlwaysOnTop: true,
	}

	win, err := pixelgl.NewWindow(cfg)
	if err != nil {
		panic(err)
	}

	if runtime.GOOS == "windows" {
		go func() {
			title, err := syscall.UTF16PtrFromString("colorex")
			if err != nil {
				return
			}

			for range 5 {
				handle, _, _ := findWindow.Call(0, uintptr(unsafe.Pointer(title)))
				if handle != 0 {
					windowHandle = handle
					setTopMost(isAlwaysOnTop)
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
		}()
	}

	ttf, err := truetype.Parse(goregular.TTF)
	if err != nil {
		panic(err)
	}

	face := truetype.NewFace(ttf, &truetype.Options{Size: 15})
	atlas := text.NewAtlas(face, text.ASCII)
	txt := text.New(pixel.V(10, 230), atlas)

	colorPreview := pixel.R(10, 190, 80, 230)
	alwaysOnTopCheckbox := Checkbox{
		bounds:  pixel.R(90, 190, 115, 215),
		checked: isAlwaysOnTop,
		label:   "Always on top",
	}

	imd := imdraw.New(nil)
	checkboxImd := imdraw.New(nil)

	const (
		boxHeight  = 25
		startY     = 150
		labelWidth = 60
		boxWidth   = 300
	)

	var lastClick time.Time
	colorBoxes := make([]ColorBox, 6)

	for !win.Closed() {
		win.Clear(pixel.RGB(0.98, 0.98, 0.98))

		if win.JustPressed(pixelgl.KeySpace) {
			isPaused = !isPaused
		}

		if !isPaused {
			lastX, lastY = robotgo.Location()

			rInt, gInt, bInt = getAccuratePixelColor(lastX, lastY)
			colorInfo = getColorInfo(rInt, gInt, bInt)
		}

		for i := range 6 {
			y := startY - float64(i)*boxHeight
			colorBoxes[i] = ColorBox{
				bounds:      pixel.R(10, y, boxWidth, y+boxHeight),
				label:       []string{"RGB:", "RGBA:", "HEX:", "HSL:", "CMYK:", "HSV:"}[i],
				value:       []string{colorInfo.RGB, colorInfo.RGBA, colorInfo.HEX, colorInfo.HSL, colorInfo.CMYK, colorInfo.HSV}[i],
				valueBounds: pixel.R(10+labelWidth, y, boxWidth, y+boxHeight),
			}
		}

		txt.Clear()

		for _, box := range colorBoxes {
			drawColorBox(win, imd, txt, box, &lastClick)
		}

		drawColorPreview(imd, colorPreview, rInt, gInt, bInt)
		imd.Draw(win)

		if runtime.GOOS == "windows" {
			drawCheckbox(win, checkboxImd, txt, &alwaysOnTopCheckbox)
		}

		txt.Draw(win, pixel.IM)
		win.Update()
		time.Sleep(UPDATE_DELAY)
	}
}

func clamp(x, min, max int) int {
	if x < min {
		return min
	}
	if x > max {
		return max
	}
	return x
}

func drawColorBox(win *pixelgl.Window, imd *imdraw.IMDraw, txt *text.Text, box ColorBox, lastClick *time.Time) {
	imd.Clear()
	imd.Color = pixel.RGB(0.95, 0.95, 0.95)
	imd.Push(box.bounds.Min, box.bounds.Max)
	imd.Rectangle(1)

	if box.valueBounds.Contains(win.MousePosition()) {
		imd.Color = pixel.RGB(0.9, 0.9, 1.0)
		imd.Push(box.bounds.Min, box.bounds.Max)
		imd.Rectangle(0)

		if win.JustPressed(pixelgl.MouseButtonLeft) {
			if time.Since(*lastClick) > CLICK_DELAY {
				robotgo.WriteAll(box.value)
				*lastClick = time.Now()
			}
		}
	}
	imd.Draw(win)

	txt.Color = pixel.RGB(0.2, 0.2, 0.2)
	txt.Dot = pixel.V(box.bounds.Min.X+8, box.bounds.Min.Y+8)
	fmt.Fprintf(txt, "%s", box.label)

	txt.Color = pixel.RGB(0.1, 0.2, 0.5)
	txt.Dot = pixel.V(box.valueBounds.Min.X, box.bounds.Min.Y+8)
	fmt.Fprintf(txt, "%s", box.value)
}

func drawColorPreview(imd *imdraw.IMDraw, colorPreview pixel.Rect, r, g, b uint8) {
	imd.Clear()
	imd.Color = pixel.RGB(0.9, 0.9, 0.9)
	imd.Push(pixel.V(colorPreview.Min.X+2, colorPreview.Min.Y-2),
		pixel.V(colorPreview.Max.X+2, colorPreview.Max.Y-2))
	imd.Rectangle(0)

	imd.Color = pixel.RGB(float64(r)/255, float64(g)/255, float64(b)/255)
	imd.Push(colorPreview.Min, colorPreview.Max)
	imd.Rectangle(0)
}

func drawCheckbox(win *pixelgl.Window, imd *imdraw.IMDraw, txt *text.Text, checkbox *Checkbox) {
	if checkbox.bounds.Contains(win.MousePosition()) {
		imd.Color = pixel.RGB(0.9, 0.9, 0.95)
		imd.Push(checkbox.bounds.Min, checkbox.bounds.Max)
		imd.Rectangle(0)

		if win.JustPressed(pixelgl.MouseButtonLeft) {
			checkbox.checked = !checkbox.checked
			isAlwaysOnTop = checkbox.checked
			go setTopMost(isAlwaysOnTop)
		}
	}

	imd.Color = pixel.RGB(0.7, 0.7, 0.7)
	imd.Push(checkbox.bounds.Min, checkbox.bounds.Max)
	imd.Rectangle(2)

	imd.Color = pixel.RGB(0.95, 0.95, 0.95)
	imd.Push(
		pixel.V(checkbox.bounds.Min.X+2, checkbox.bounds.Min.Y+2),
		pixel.V(checkbox.bounds.Max.X-2, checkbox.bounds.Max.Y-2),
	)
	imd.Rectangle(0)

	if checkbox.checked {
		imd.Color = pixel.RGB(0.2, 0.5, 0.8)
		imd.Push(
			pixel.V(checkbox.bounds.Min.X+8, checkbox.bounds.Min.Y+8),
			pixel.V(checkbox.bounds.Max.X-8, checkbox.bounds.Max.Y-8),
		)
		imd.Rectangle(0)
	}

	imd.Draw(win)

	txt.Color = pixel.RGB(0.2, 0.2, 0.2)
	txt.Dot = pixel.V(checkbox.bounds.Max.X+8, checkbox.bounds.Min.Y+8)
	fmt.Fprintf(txt, "%s", checkbox.label)
}

func main() {
	pixelgl.Run(run)
}
