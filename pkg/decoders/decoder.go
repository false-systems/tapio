package decoders

import (
	"errors"
	"fmt"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
)

// ErrSkipLabelSet instructs exporter to skip label set
var ErrSkipLabelSet = errors.New("this label set should be skipped")

// DecoderFunc transforms byte field value into a string
// Decoders can be chained: raw bytes → IP → pod name
type DecoderFunc interface {
	Decode(in []byte, conf Decoder) ([]byte, error)
}

// Set is a set of DecoderFuncs that may be applied to produce a label
type Set struct {
	mu        sync.Mutex
	decoders  map[string]DecoderFunc
	cache     map[string]map[string][]string
	skipCache *lru.Cache[string, struct{}]
}

// NewSet creates a Set with basic decoders
func NewSet(skipCacheSize int) (*Set, error) {
	s := &Set{
		decoders: map[string]DecoderFunc{
			"inet_ip":    &InetIP{},
			"string":     &String{},
			"static_map": &StaticMap{},
			"syscall":    &Syscall{},
		},
		cache: map[string]map[string][]string{},
	}

	if skipCacheSize > 0 {
		skipCache, err := lru.New[string, struct{}](skipCacheSize)
		if err != nil {
			return nil, err
		}
		s.skipCache = skipCache
	}
	return s, nil
}

// RegisterDecoder adds a decoder to the set (for K8s decoders)
func (s *Set) RegisterDecoder(name string, decoder DecoderFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decoders[name] = decoder
}

// decode transforms input byte field into a string according to configuration
func (s *Set) decode(in []byte, label Label) ([]byte, error) {
	result := in

	for _, decoder := range label.Decoders {
		if _, ok := s.decoders[decoder.Name]; !ok {
			return result, fmt.Errorf("unknown decoder %q", decoder.Name)
		}

		decoded, err := s.decoders[decoder.Name].Decode(result, decoder)
		if err != nil {
			if errors.Is(err, ErrSkipLabelSet) {
				if s.skipCache != nil {
					s.skipCache.Add(string(in), struct{}{})
				}
				return decoded, err
			}

			return decoded, fmt.Errorf("error decoding with decoder %q: %w", decoder.Name, err)
		}

		result = decoded
	}

	return result, nil
}

// DecodeLabelsForMetrics transforms eBPF map key bytes into a list of label values
// according to configuration (different label sets require different names).
// This decoder method variant does caching and is suitable for metrics.
func (s *Set) DecodeLabelsForMetrics(in []byte, name string, labels []Label) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cache, ok := s.cache[name]
	if !ok {
		cache = map[string][]string{}
		s.cache[name] = cache
	}

	// string(in) must not be a variable to avoid allocation:
	// * https://github.com/golang/go/commit/f5f5a8b6209f8
	if cached, ok := cache[string(in)]; ok {
		return cached, nil
	}

	// Also check the skip cache if the input would have return ErrSkipLabelSet
	// and return the error early.
	if s.skipCache != nil {
		if _, ok := s.skipCache.Get(string(in)); ok {
			return nil, ErrSkipLabelSet
		}
	}

	values, err := s.decodeLabels(in, labels)
	if err != nil {
		return nil, err
	}

	cache[string(in)] = values

	return values, nil
}

// DecodeLabelsForTracing transforms eBPF map key bytes into a list of label values
// according to configuration (different label sets require different names).
// This decoder method variant does not do caching and is suitable for tracing.
func (s *Set) DecodeLabelsForTracing(in []byte, labels []Label) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.decodeLabels(in, labels)
}

// decodeLabels is the inner function of DecodeLabels without any caching
func (s *Set) decodeLabels(in []byte, labels []Label) ([]string, error) {
	values := make([]string, len(labels))

	off := uint(0)

	totalSize := uint(0)
	for _, label := range labels {
		size := label.Size
		if size == 0 {
			return nil, fmt.Errorf("error decoding label %q: size is zero or not set", label.Name)
		}

		totalSize += size + label.Padding
	}

	if totalSize != uint(len(in)) {
		return nil, fmt.Errorf("error decoding labels: total size of key %#v is %d bytes, but we have labels to decode %d", in, len(in), totalSize)
	}

	for i, label := range labels {
		if len(label.Decoders) == 0 {
			return nil, fmt.Errorf("error decoding label %q: no decoders set", label.Name)
		}

		size := label.Size

		decoded, err := s.decode(in[off:off+size], label)
		if err != nil {
			return nil, err
		}

		off += size + label.Padding

		values[i] = string(decoded)
	}

	return values, nil
}
