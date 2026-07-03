package main

import (
	"fmt"

	"github.com/lxn/walk"
)

func main() {
	walk.App().SetOrganizationName("DesktopGo")
	walk.App().SetProductName("DesktopGo")

	// 测试文本
	text := "测试文本 Test"

	// 创建一个内存画布
	bmp, err := walk.NewBitmapForDPI(walk.Size{Width: 1, Height: 1}, 96)
	if err != nil {
		fmt.Printf("创建位图失败: %v\n", err)
		return
	}
	defer bmp.Dispose()

	canvas, err := walk.NewCanvasFromImage(bmp)
	if err != nil {
		fmt.Printf("创建画布失败: %v\n", err)
		return
	}
	defer canvas.Dispose()

	// 创建字体
	font, err := walk.NewFont("微软雅黑", 9, 0 /* FontRegular */)
	if err != nil {
		fmt.Printf("创建字体失败: %v\n", err)
		return
	}
	defer font.Dispose()

	// 使用 MeasureTextPixels 进行测量
	//format := walk.DrawTextFormat(walk.TextSingleLine | walk.TextNoPrefix)
	bounds, _, err := canvas.MeasureTextPixels(text, font, walk.Rectangle{}, 0)
	if err != nil {
		fmt.Printf("测量失败: %v\n", err)
		return
	}

	fmt.Printf("文本: %s\n", text)
	fmt.Printf("实际尺寸：宽 = %d, 高 = %d\n", bounds.Width, bounds.Height)
}
