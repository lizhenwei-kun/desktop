package main

import (
	"fmt"
	"syscall"

	"github.com/lxn/win"
)

func getTextExtent(hdc win.HDC, text string, hFont win.HFONT) (int, int) {
	oldFont := win.SelectObject(hdc, win.HGDIOBJ(hFont))
	if oldFont == 0 {
		return 0, 0
	}
	defer win.SelectObject(hdc, oldFont)

	var size win.SIZE
	textUTF16, _ := syscall.UTF16PtrFromString(text)
	win.GetTextExtentPoint32(hdc, textUTF16, int32(len(text)), &size)
	return int(size.CX), int(size.CY)
}

func createFont(family string, pointSize int, dpi int) win.HFONT {
	var lf win.LOGFONT
	lf.LfHeight = -win.MulDiv(int32(pointSize), int32(dpi), 72)
	lf.LfWeight = win.FW_NORMAL
	lf.LfCharSet = win.DEFAULT_CHARSET
	lf.LfOutPrecision = win.OUT_TT_PRECIS
	lf.LfClipPrecision = win.CLIP_DEFAULT_PRECIS
	lf.LfQuality = win.CLEARTYPE_QUALITY
	lf.LfPitchAndFamily = win.VARIABLE_PITCH | win.FF_SWISS

	src, _ := syscall.UTF16FromString(family)
	copy(lf.LfFaceName[:], src)

	return win.CreateFontIndirect(&lf)
}

func main() {
	text := "Test"

	// 创建内存 DC
	hdc := win.CreateCompatibleDC(0)
	if hdc == 0 {
		fmt.Println("CreateCompatibleDC 失败")
		return
	}
	defer win.DeleteDC(hdc)

	// 创建字体（使用系统级 Win32 API）
	hFont := createFont("微软雅黑", 9, 96)
	if hFont == 0 {
		fmt.Println("创建字体失败")
		return
	}
	defer win.DeleteObject(win.HGDIOBJ(hFont))

	// 测量文本
	w, h := getTextExtent(hdc, text, hFont)

	fmt.Printf("文本: %s\n", text)
	fmt.Printf("实际尺寸：宽 = %d, 高 = %d\n", w, h)

	// 阻止立即退出（便于查看结果）
	//fmt.Println("\n按 Enter 键退出...")
	//fmt.Scanln()
}
