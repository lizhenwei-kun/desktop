package ui

import (
	"image"
	"image/color"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
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

// ShowColorDialog 显示颜色选择对话框
func ShowColorDialog(owner walk.Form, title string, presetColors []string) (string, bool) {
	var dlg *walk.Dialog
	var result string
	var accepted bool
	var customEdit *walk.LineEdit

	// 颜色方块（使用 CustomWidget 绘制实际颜色，避免 Composite.OnMouseDown 在对话框中的潜在问题）
	const squareSize = 48

	var colorWidgets []Widget
	for _, c := range presetColors {
		colorStr := c
		parsed := ParseHexColor(c)

		swatch := CustomWidget{
			MinSize:     Size{Width: squareSize, Height: squareSize},
			MaxSize:     Size{Width: squareSize, Height: squareSize},
			ToolTipText: colorStr,
			PaintPixels: func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
				// 与 group_card.go/createColorBitmap 同样的方式：创建纯色 RGBA 图像
				rect := canvas.Bounds()
				if rect.Width <= 0 || rect.Height <= 0 {
					rect = walk.Rectangle{Width: squareSize, Height: squareSize}
				}
				img := image.NewRGBA(image.Rect(0, 0, rect.Width, rect.Height))
				for y := 0; y < rect.Height; y++ {
					for x := 0; x < rect.Width; x++ {
						img.SetRGBA(x, y, color.RGBA{R: parsed.R, G: parsed.G, B: parsed.B, A: 255})
					}
				}
				bmp, err := walk.NewBitmapFromImage(img)
				if err != nil {
					return err
				}
				defer bmp.Dispose()
				canvas.DrawBitmapWithOpacityPixels(bmp, rect, 255)
				return nil
			},
			OnMouseDown: func(x, y int, button walk.MouseButton) {
				if button == walk.LeftButton {
					result = colorStr
					accepted = true
					dlg.Accept()
				}
			},
		}
		colorWidgets = append(colorWidgets, swatch)
	}

	Dialog{
		AssignTo:   &dlg,
		Title:      title,
		MinSize:    Size{Width: 340, Height: 260},
		Background: SolidColorBrush{Color: walk.RGB(0x1A, 0x1A, 0x2E)},
		Layout:     VBox{Margins: Margins{Left: 20, Top: 20, Right: 20, Bottom: 20}, Spacing: 16},
		Children: []Widget{
			Label{
				Text:      "选择颜色",
				TextColor: walk.RGB(0xFF, 0xFF, 0xFF),
				Font:      Font{Family: "Microsoft YaHei", PointSize: 14, Bold: true},
			},
			Composite{
				Layout:   Flow{Spacing: 12},
				Children: colorWidgets,
			},
			VSpacer{},
			Composite{
				Layout: HBox{Spacing: 8},
				Children: []Widget{
					LineEdit{
						AssignTo:    &customEdit,
						Text:        "#RRGGBBAA",
						TextColor:   walk.RGB(0xFF, 0xFF, 0xFF),
						Background:  SolidColorBrush{Color: walk.RGB(0x30, 0x34, 0x3C)},
						MinSize:     Size{Width: 180, Height: 32},
						ToolTipText: "输入自定义颜色值，如 #276BA6B8",
					},
					PushButton{
						Text: "自定义",
						OnClicked: func() {
							if customEdit != nil {
								result = customEdit.Text()
								accepted = true
								dlg.Accept()
							}
						},
					},
					HSpacer{},
					PushButton{
						Text:      "取消",
						OnClicked: func() { dlg.Cancel() },
					},
				},
			},
		},
	}.Create(owner)

	dlg.Run()
	return result, accepted
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
