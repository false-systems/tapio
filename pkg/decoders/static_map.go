package decoders

import "fmt"

// StaticMap is a decoded that maps values according to a static map
type StaticMap struct{}

// Decode maps values according to a static map
func (s *StaticMap) Decode(in []byte, conf Decoder) ([]byte, error) {
	if conf.StaticMap == nil {
		return []byte("empty mapping"), nil
	}

	value, ok := conf.StaticMap[string(in)]
	if !ok {
		if conf.AllowUnknown {
			return in, nil
		}
		return []byte(fmt.Sprintf("unknown:%s", in)), nil
	}

	return []byte(value), nil
}
