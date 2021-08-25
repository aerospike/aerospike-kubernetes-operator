package configschema

import _ "embed"

//go:embed jsonfiles/4_0_0.json
var conf4_0_0 string

//go:embed jsonfiles/4_1_0.json
var conf4_1_0 string

//go:embed jsonfiles/4_2_0.json
var conf4_2_0 string

//go:embed jsonfiles/4_3_0.json
var conf4_3_0 string

//go:embed jsonfiles/4_3_1.json
var conf4_3_1 string

//go:embed jsonfiles/4_4_0.json
var conf4_4_0 string

//go:embed jsonfiles/4_5_0.json
var conf4_5_0 string

//go:embed jsonfiles/4_5_1.json
var conf4_5_1 string

//go:embed jsonfiles/4_5_2.json
var conf4_5_2 string

//go:embed jsonfiles/4_5_3.json
var conf4_5_3 string

//go:embed jsonfiles/4_6_0.json
var conf4_6_0 string

//go:embed jsonfiles/4_7_0.json
var conf4_7_0 string

//go:embed jsonfiles/4_7_0.json
var conf4_8_0 string

//go:embed jsonfiles/4_9_0.json
var conf4_9_0 string

//go:embed jsonfiles/5_0_0.json
var conf5_0_0 string

//go:embed jsonfiles/5_1_0.json
var conf5_1_0 string

//go:embed jsonfiles/5_2_0.json
var conf5_2_0 string

//go:embed jsonfiles/5_3_0.json
var conf5_3_0 string

//go:embed jsonfiles/5_4_0.json
var conf5_4_0 string

//go:embed jsonfiles/5_5_0.json
var conf5_5_0 string

//go:embed jsonfiles/5_6_0.json
var conf5_6_0 string

// SchemaMap has all supported json schema in string form
var SchemaMap = map[string]string{
	"4.9.0": conf4_9_0,
	"5.0.0": conf5_0_0,
	"5.1.0": conf5_1_0,
	"5.2.0": conf5_2_0,
	"5.3.0": conf5_3_0,
	"5.4.0": conf5_4_0,
	"5.5.0": conf5_5_0,
	"5.6.0": conf5_6_0,
}
