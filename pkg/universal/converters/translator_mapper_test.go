package converters

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/falseyair/tapio/pkg/translator"
	"github.com/falseyair/tapio/pkg/universal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockTranslatorEngine is a mock implementation of translator.Engine
type MockTranslatorEngine struct {
	mock.Mock
}

func (m *MockTranslatorEngine) Translate(pid int32) (*translator.TranslationResult, error) {
	args := m.Called(pid)
	result := args.Get(0)
	if result == nil {
		return nil, args.Error(1)
	}
	return result.(*translator.TranslationResult), args.Error(1)
}

func (m *MockTranslatorEngine) GetMetrics() (cacheHitRate float64, avgLatency float64) {
	args := m.Called()
	return args.Get(0).(float64), args.Get(1).(float64)
}

func (m *MockTranslatorEngine) Start(ctx interface{}) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockTranslatorEngine) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func TestNewTranslatorPIDMapper(t *testing.T) {
	engine := &MockTranslatorEngine{}
	mapper := NewTranslatorPIDMapper(engine)

	assert.NotNil(t, mapper)
	assert.Equal(t, engine, mapper.engine)
	assert.NotNil(t, mapper.fallbackData)
}

func TestMapPIDToTarget(t *testing.T) {
	t.Run("successful_translation", func(t *testing.T) {
		engine := &MockTranslatorEngine{}
		mapper := NewTranslatorPIDMapper(engine)

		expectedResult := &translator.TranslationResult{
			PodName:       "test-pod",
			Namespace:     "default",
			ContainerID:   "abc123",
			ContainerName: "app",
			NodeName:      "node1",
			Timestamp:     time.Now(),
		}

		engine.On("Translate", int32(1234)).Return(expectedResult, nil)

		target, err := mapper.MapPIDToTarget(1234)

		assert.NoError(t, err)
		assert.NotNil(t, target)
		assert.Equal(t, universal.TargetTypePod, target.Type)
		assert.Equal(t, "test-pod", target.Name)
		assert.Equal(t, "default", target.Namespace)
		assert.Equal(t, int32(1234), target.PID)
		assert.Equal(t, "app", target.Container)
		assert.Equal(t, "node1", target.Node)
		// Container ID is not stored in Target.Labels, but in the result metadata

		engine.AssertExpectations(t)

		// Verify fallback was updated
		mapper.mu.RLock()
		fallback, exists := mapper.fallbackData[1234]
		mapper.mu.RUnlock()
		assert.True(t, exists)
		assert.Equal(t, target, fallback)
	})

	t.Run("translation_error_with_fallback", func(t *testing.T) {
		engine := &MockTranslatorEngine{}
		mapper := NewTranslatorPIDMapper(engine)

		// Pre-populate fallback
		fallbackTarget := &universal.Target{
			Type:      universal.TargetTypePod,
			Name:      "fallback-pod",
			Namespace: "fallback-ns",
			PID:       5678,
		}
		mapper.fallbackData[5678] = fallbackTarget

		engine.On("Translate", int32(5678)).Return(nil, errors.New("translation failed"))

		target, err := mapper.MapPIDToTarget(5678)

		assert.NoError(t, err)
		assert.Equal(t, fallbackTarget, target)

		engine.AssertExpectations(t)
	})

	t.Run("translation_error_no_fallback", func(t *testing.T) {
		engine := &MockTranslatorEngine{}
		mapper := NewTranslatorPIDMapper(engine)

		engine.On("Translate", int32(9999)).Return(nil, errors.New("translation failed"))

		target, err := mapper.MapPIDToTarget(9999)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "translation failed")
		assert.Nil(t, target)

		engine.AssertExpectations(t)
	})

	t.Run("no_translator_engine", func(t *testing.T) {
		mapper := NewTranslatorPIDMapper(nil)

		target, err := mapper.MapPIDToTarget(1111)

		assert.NoError(t, err)
		assert.NotNil(t, target)
		assert.Equal(t, universal.TargetTypeProcess, target.Type)
		assert.Equal(t, "process-1111", target.Name)
		assert.Equal(t, int32(1111), target.PID)
	})
}

func TestUpdateMapping(t *testing.T) {
	mapper := NewTranslatorPIDMapper(nil)

	target := &universal.Target{
		Type:      universal.TargetTypePod,
		Name:      "manual-pod",
		Namespace: "manual-ns",
		PID:       2222,
	}

	mapper.UpdateMapping(2222, target)

	mapper.mu.RLock()
	stored, exists := mapper.fallbackData[2222]
	mapper.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, target, stored)
}

func TestClearCache(t *testing.T) {
	mapper := NewTranslatorPIDMapper(nil)

	// Add some data
	mapper.fallbackData[1111] = &universal.Target{Name: "pod1"}
	mapper.fallbackData[2222] = &universal.Target{Name: "pod2"}

	assert.Len(t, mapper.fallbackData, 2)

	mapper.ClearCache()

	assert.Len(t, mapper.fallbackData, 0)
}

func TestCleanupOldEntries(t *testing.T) {
	mapper := NewTranslatorPIDMapper(nil)

	// Currently returns 0 as timestamp tracking is not implemented
	removed := mapper.CleanupOldEntries(5 * time.Minute)

	assert.Equal(t, 0, removed)
}

func TestConcurrentMapperAccess(t *testing.T) {
	engine := &MockTranslatorEngine{}
	mapper := NewTranslatorPIDMapper(engine)

	// Setup mock to handle concurrent calls
	engine.On("Translate", mock.Anything).Return(nil, errors.New("test error"))

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(pid int32) {
			// Try to map
			mapper.MapPIDToTarget(pid)

			// Update mapping
			target := &universal.Target{
				Type: universal.TargetTypeProcess,
				Name: fmt.Sprintf("process-%d", pid),
				PID:  pid,
			}
			mapper.UpdateMapping(pid, target)

			done <- true
		}(int32(i))
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all mappings were stored
	mapper.mu.RLock()
	count := len(mapper.fallbackData)
	mapper.mu.RUnlock()

	assert.Equal(t, 10, count)
}
