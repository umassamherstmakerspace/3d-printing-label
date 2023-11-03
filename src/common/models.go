package models

type Print struct {
	HumanName      string `json:"humanName" xml:"humanName" form:"humanName" query:"humanName" validate:"required"`
	FileName       string `json:"fileName" xml:"fileName" form:"fileName" query:"fileName" validate:"required"`
	FilamentType   string `json:"filamentType" xml:"filamentType" form:"filamentType" query:"filamentType" validate:"required"`
	FilamentOwner  string `json:"filamentOwner" xml:"filamentOwner" form:"filamentOwner" query:"filamentOwner" validate:"required"`
	FilamentWeight string `json:"filamentWeight" xml:"filamentWeight" form:"filamentWeight" query:"filamentWeight" validate:"required"`
	Time           string `json:"time" xml:"time" form:"time" query:"time" validate:"required"`
	UUID           string `json:"uuid" xml:"uuid" form:"uuid" query:"uuid" validate:"required"`
	Timestamp      int64  `json:"timestamp" xml:"timestamp" form:"timestamp" query:"timestamp" validate:"required"`
}

func (p *Print) GenerateLabelZPL() string {
	output := "^XA"

	output += "\n\n^FX First Section with person name and print name"
	output += "\n^CF0,50"
	output += "\n^FO50,50^FDName:^FS"
	output += "\n^CFD,40"
	output += "\n^FO200,60^FD" + p.HumanName + "^FS"

	output += "\n\n^CF0,50"
	output += "\n^FO50,120^FDFile:^FS"
	output += "\n^CFD,40"
	output += "\n^FO180,130^FD" + p.FileName + "^FS"

	output += "\n\n^FO50,200^GB1100,3,3^FS"

	// ------------------------------------------------

	output += "\n\n\n^FX Second Section with plastic type"
	output += "\n^CF0,50"
	output += "\n^FO50,250^FDFilament:^FS"
	output += "\n^CFD,40"
	output += "\n^FO260,260^FD" + p.FilamentType + "^FS"

	output += "\n\n^CF0,50"
	output += "\n^FO50,320^FDType:^FS"
	output += "\n^CFD,40"
	output += "\n^FO180,330^FD" + p.FilamentOwner + "^FS"

	output += "\n\n^CF0,50"
	output += "\n^FO50,390^FDWeight:^FS"
	output += "\n^CFD,40"
	output += "\n^FO220,400^FD" + p.FilamentWeight + "^FS"

	output += "\n\n^FO50,470^GB1100,3,3^FS"

	// ------------------------------------------------

	output += "\n\n\n^FX Third Section with time and QR"

	output += "\n^CFD,40"
	output += "\n^FO50,820^FD" + p.Time + "^FS"

	output += "\n\n^FO900,600"
	output += "\n^BQN,2,8"
	output += "\n^FDMA," + p.UUID + "^FS"

	output += "\n\n^XZ"

	return output
}
