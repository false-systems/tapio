#ifndef __BPF_BATCH_H__
#define __BPF_BATCH_H__

#include "vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

#define MAX_BATCH_SIZE 64
#define BATCH_TIMEOUT_NS 100000000 // 100ms

// Batch buffer structure
struct batch_buffer {
    __u32 count;                    // Current number of events in batch
    __u32 max_size;                  // Maximum batch size
    __u64 first_event_ns;            // Timestamp of first event in batch
    __u64 last_flush_ns;             // Last flush timestamp
    __u8 data[8192];                 // Buffer for batched events
    __u32 data_offset;               // Current offset in data buffer
};

// Per-CPU batch buffer map
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct batch_buffer);
} batch_buffers SEC(".maps");

// Batch configuration
struct batch_config {
    __u32 enabled;                   // Batching enabled flag
    __u32 max_batch_size;            // Maximum events per batch
    __u64 batch_timeout_ns;          // Timeout in nanoseconds
    __u32 min_batch_size;            // Minimum batch size before flush
};

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct batch_config);
} batch_configuration SEC(".maps");

// Initialize batch buffer
static __always_inline void batch_buffer_init(struct batch_buffer *batch)
{
    if (!batch)
        return;
    
    batch->count = 0;
    batch->data_offset = 0;
    batch->first_event_ns = 0;
    batch->last_flush_ns = bpf_ktime_get_ns();
}

// Check if batch should be flushed
static __always_inline __u8 should_flush_batch(struct batch_buffer *batch, struct batch_config *config)
{
    if (!batch || !config || !config->enabled)
        return 1; // Flush immediately if batching disabled
    
    __u64 now = bpf_ktime_get_ns();
    
    // Check if batch is full
    if (batch->count >= config->max_batch_size)
        return 1;
    
    // Check if timeout exceeded
    if (batch->count > 0 && batch->first_event_ns > 0) {
        __u64 elapsed = now - batch->first_event_ns;
        if (elapsed >= config->batch_timeout_ns)
            return 1;
    }
    
    // Check minimum batch size for efficiency
    if (batch->count >= config->min_batch_size) {
        __u64 since_last_flush = now - batch->last_flush_ns;
        if (since_last_flush >= (config->batch_timeout_ns / 2))
            return 1;
    }
    
    return 0;
}

// Add event to batch
static __always_inline int batch_add_event(struct batch_buffer *batch, 
                                           void *event_data, 
                                           __u32 event_size)
{
    if (!batch || !event_data)
        return -1;
    
    // Check if there's enough space
    if (batch->data_offset + event_size > sizeof(batch->data))
        return -1; // Buffer full
    
    // Ensure we don't exceed bounds (verifier requirement)
    if (event_size > 512) // Reasonable max event size
        return -1;
    
    // Copy event data to batch buffer
    if (bpf_probe_read_kernel(&batch->data[batch->data_offset], 
                              event_size & 511, // Bounded size for verifier
                              event_data) < 0)
        return -1;
    
    // Update batch metadata
    if (batch->count == 0) {
        batch->first_event_ns = bpf_ktime_get_ns();
    }
    
    batch->data_offset += event_size;
    batch->count++;
    
    return 0;
}

// Flush batch to ring buffer
static __always_inline int batch_flush(void *ringbuf_map, struct batch_buffer *batch)
{
    if (!batch || batch->count == 0)
        return 0;
    
    // Reserve space in ring buffer for entire batch
    void *rb_data = bpf_ringbuf_reserve(ringbuf_map, 
                                        batch->data_offset + sizeof(__u32), 
                                        0);
    if (!rb_data)
        return -1;
    
    // Write batch header (count)
    *(__u32 *)rb_data = batch->count;
    
    // Copy batch data
    if (batch->data_offset > 0 && batch->data_offset <= sizeof(batch->data)) {
        __builtin_memcpy(rb_data + sizeof(__u32), batch->data, batch->data_offset);
    }
    
    // Submit the batch
    bpf_ringbuf_submit(rb_data, 0);
    
    // Reset batch buffer
    batch->count = 0;
    batch->data_offset = 0;
    batch->first_event_ns = 0;
    batch->last_flush_ns = bpf_ktime_get_ns();
    
    return 0;
}

// Helper macro for batch processing
#define BATCH_PROCESS_EVENT(ringbuf_map, event_data, event_size) \
    ({ \
        __u32 __key = 0; \
        struct batch_buffer *__batch = bpf_map_lookup_elem(&batch_buffers, &__key); \
        struct batch_config *__config = bpf_map_lookup_elem(&batch_configuration, &__key); \
        int __ret = 0; \
        \
        if (__batch && __config) { \
            if (__config->enabled) { \
                if (should_flush_batch(__batch, __config)) { \
                    batch_flush(ringbuf_map, __batch); \
                } \
                __ret = batch_add_event(__batch, event_data, event_size); \
                if (__ret < 0) { \
                    batch_flush(ringbuf_map, __batch); \
                    __ret = batch_add_event(__batch, event_data, event_size); \
                } \
            } else { \
                /* Direct send if batching disabled */ \
                void *__rb = bpf_ringbuf_reserve(ringbuf_map, event_size, 0); \
                if (__rb) { \
                    __builtin_memcpy(__rb, event_data, event_size); \
                    bpf_ringbuf_submit(__rb, 0); \
                } else { \
                    __ret = -1; \
                } \
            } \
        } \
        __ret; \
    })

#endif // __BPF_BATCH_H__