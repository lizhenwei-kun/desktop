package ui

import (
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

// Windows CHOOSECOLOR 相关定义
const (
	ccRGBInit    = 0x00000001
	ccFullOpen   = 0x00000002
	ccEnableHook = 0x00000010
	ccAnyColor   = 0x00000100

	swpNoSize     = 0x0001
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	spiGetWorkArea = 0x0030
)

type chooseColorW struct {
	lStructSize    uint32
	hwndOwner      win.HWND
	hInstance      win.HINSTANCE
	rgbResult      uint32
	lpCustColors   *uint32
	flags          uint32
	lCustData      uintptr
	lpfnHook       uintptr
	lpTemplateName *uint16
}

var (
	modComdlg32             = syscall.NewLazyDLL("comdlg32.dll")
	procChooseColorW        = modComdlg32.NewProc("ChooseColorW")
)

// ShowInputDialog 显示文本输入对话框
func ShowInputDialog(owner walk.Form, title, label, defaultValue string) (string, bool) {
	var dlg *walk.Dialog
	var lineEdit *walk.LineEdit
	var result string
	var accepted bool

	Dialog{
		AssignTo: &dlg,
		Title:    title,
		MinSize:  Size{Width: 300, Height: 120},
		Layout:   VBox{},
		Children: []Widget{
			Label{Text: label},
			LineEdit{
				AssignTo: &lineEdit,
				Text:     defaultValue,
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						Text: "确定",
						OnClicked: func() {
							result = lineEdit.Text()
							accepted = true
							dlg.Accept()
						},
					},
					PushButton{
						Text: "取消",
						OnClicked: func() {
							dlg.Cancel()
						},
					},
				},
			},
		},
	}.Create(owner)

	dlg.Run()
	return result, accepted
}

// ShowConfirmDialog 显示确认对话框
func ShowConfirmDialog(owner walk.Form, title, message string) bool {
	result := walk.MsgBox(owner, title, message, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion)
	return result == walk.DlgCmdYes
}

// ShowColorDialog 显示系统颜色选择对话框（ChooseColorW）
func ShowColorDialog(owner walk.Form, title string, presetColors []string) (string, bool) {
	// 初始化自定义颜色数组（16 个 COLORREF，初始为白色）
	var custColors [16]uint32
	for i := range custColors {
		custColors[i] = 0x00FFFFFF
	}

	// 如果有预设颜色，取第一个作为初始选中颜色
	var initColor uint32
	if len(presetColors) > 0 {
		c := ParseHexColor(presetColors[0])
		initColor = uint32(c.R) | uint32(c.G)<<8 | uint32(c.B)<<16
	}

	var hwndOwner win.HWND
	if owner != nil {
		hwndOwner = owner.Handle()
	}

	// 钩子回调：WM_INITDIALOG 中将对话框居中于屏幕中央
	hookProc := syscall.NewCallback(func(hDlg win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
		if msg == 0x0110 /* WM_INITDIALOG */ {
			var dlgRect, desktopRect win.RECT
			win.GetWindowRect(hDlg, &dlgRect)
			win.GetWindowRect(win.GetDesktopWindow(), &desktopRect)

			dlgW := dlgRect.Right - dlgRect.Left
			dlgH := dlgRect.Bottom - dlgRect.Top

			x := (desktopRect.Right - dlgW) / 2
			y := (desktopRect.Bottom - dlgH) / 2

			win.SetWindowPos(hDlg, 0, x, y, 0, 0,
				swpNoSize|swpNoZOrder)
		}
		return 0
	})

	cc := chooseColorW{
		lStructSize:    uint32(unsafe.Sizeof(chooseColorW{})),
		hwndOwner:      hwndOwner,
		hInstance:      0,
		rgbResult:      initColor,
		lpCustColors:   &custColors[0],
		flags:          ccRGBInit | ccFullOpen | ccAnyColor | ccEnableHook,
		lCustData:      0,
		lpfnHook:       hookProc,
		lpTemplateName: nil,
	}

	ret, _, _ := procChooseColorW.Call(uintptr(unsafe.Pointer(&cc)))
	if ret == 0 {
		return "", false
	}

	// 将 COLORREF (0x00BBGGRR) 转换为 #RRGGBBAA 格式
	r := byte(cc.rgbResult & 0xFF)
	g := byte((cc.rgbResult >> 8) & 0xFF)
	b := byte((cc.rgbResult >> 16) & 0xFF)
	return "#" + hexByte(r) + hexByte(g) + hexByte(b) + "FF", true
}

// PresetColors 预设颜色方案
var PresetColors = []string{
	"#342333B8", // 深紫
	"#A783BEB8", // 淡紫
	"#24A892B8", // 青绿
	"#276BA6B8", // 蓝色
	"#C54834B8", // 红橙
	"#1F6F5FB8", // 墨绿
	"#30343CBD", // 深灰
}
