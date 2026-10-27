package domain

func TemplateLayout(id string) (Layout, error) {
	layout := Layout{Widgets: []Widget{}}
	switch id {
	case "":
	case "small":
		layout.Widgets = []Widget{
			{ID: "cpu-value", Stream: "cpu", StreamType: "number", Kind: "value", X: 0, Y: 0, W: 4, H: 2},
			{ID: "cpu-trend", Stream: "cpu", StreamType: "number", Kind: "line", X: 4, Y: 0, W: 8, H: 3},
			{ID: "logs", Stream: "logs", StreamType: "log", Kind: "logs", X: 0, Y: 3, W: 12, H: 4},
		}
	default:
		return Layout{}, Invalid("unknown App template")
	}
	return layout, ValidateLayout(layout, nil)
}
