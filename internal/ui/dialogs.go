package ui

import (
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

	var buttons []Widget
	for _, c := range presetColors {
		color := c
		buttons = append(buttons, PushButton{
			Text:    "■",
			MaxSize: Size{Width: 40, Height: 40},
			OnClicked: func() {
				result = color
				accepted = true
				dlg.Accept()
			},
		})
	}

	// 添加自定义输入
	var customEdit *walk.LineEdit
	buttons = append(buttons,
		LineEdit{
			AssignTo:    &customEdit,
			Text:        "#RRGGBBAA",
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
	)

	Dialog{
		AssignTo: &dlg,
		Title:    title,
		MinSize:  Size{Width: 360, Height: 160},
		Layout:   VBox{},
		Children: []Widget{
			Label{Text: "选择颜色："},
			Composite{
				Layout:   Flow{},
				Children: buttons,
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
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
