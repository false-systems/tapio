package decoders

// Label describes how to extract and decode a label from eBPF data
type Label struct {
	Name     string    `yaml:"name"`
	Size     uint      `yaml:"size"`
	Padding  uint      `yaml:"padding"`
	Decoders []Decoder `yaml:"decoders"`
}

// Decoder configures a single decoder in the pipeline
type Decoder struct {
	Name         string            `yaml:"name"`
	StaticMap    map[string]string `yaml:"static_map"`
	Regexps      []string          `yaml:"regexps"`
	AllowUnknown bool              `yaml:"allow_unknown"`
}
