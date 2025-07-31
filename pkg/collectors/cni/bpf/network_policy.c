// +build ignore

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <linux/in.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>
#include <bpf/bpf_core_read.h>

// Event types
#define EVENT_POLICY_ALLOW 1
#define EVENT_POLICY_DROP  2
#define EVENT_POLICY_LOG   3

// Policy actions
#define ACTION_ALLOW 1
#define ACTION_DROP  2
#define ACTION_LOG   3

struct policy_event {
    __u64 timestamp;
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  protocol;
    __u8  action;
    __u8  direction; // 0=ingress, 1=egress
    __u8  event_type;
    char  pod_name[64];
    char  namespace[64];
    char  policy_name[64];
};

// Ring buffer for events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB ring buffer
} policy_events SEC(".maps");

// Map to track active policies
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, __u32);   // Policy ID
    __type(value, struct policy_rule);
    __uint(max_entries, 10000);
} active_policies SEC(".maps");

// Map pod IP to metadata
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key, __u32);   // IP address
    __type(value, struct pod_metadata);
    __uint(max_entries, 10000);
} pod_metadata_map SEC(".maps");

struct policy_rule {
    __u32 policy_id;
    __u32 src_cidr;
    __u32 src_mask;
    __u32 dst_cidr;
    __u32 dst_mask;
    __u16 port;
    __u8  protocol;
    __u8  action;
    char  name[64];
};

struct pod_metadata {
    char pod_name[64];
    char namespace[64];
    __u32 ip;
};

// Helper to submit event to ring buffer
static __always_inline void submit_event(struct policy_event *event) {
    event->timestamp = bpf_ktime_get_ns();
    bpf_ringbuf_output(&policy_events, event, sizeof(*event), 0);
}

// TC ingress hook for Calico
SEC("tc/ingress_calico")
int tc_ingress_calico(struct __sk_buff *skb) {
    void *data_end = (void *)(long)skb->data_end;
    void *data = (void *)(long)skb->data;
    
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;
    
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;
    
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return TC_ACT_OK;
    
    struct policy_event *event;
    event = bpf_ringbuf_reserve(&policy_events, sizeof(*event), 0);
    if (!event)
        return TC_ACT_OK;
    
    event->src_ip = ip->saddr;
    event->dst_ip = ip->daddr;
    event->protocol = ip->protocol;
    event->direction = 0; // ingress
    
    // Extract ports based on protocol
    if (ip->protocol == IPPROTO_TCP) {
        struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
        if ((void *)(tcp + 1) > data_end) {
            bpf_ringbuf_discard(event, 0);
            return TC_ACT_OK;
        }
        event->src_port = bpf_ntohs(tcp->source);
        event->dst_port = bpf_ntohs(tcp->dest);
    } else if (ip->protocol == IPPROTO_UDP) {
        struct udphdr *udp = (void *)ip + (ip->ihl * 4);
        if ((void *)(udp + 1) > data_end) {
            bpf_ringbuf_discard(event, 0);
            return TC_ACT_OK;
        }
        event->src_port = bpf_ntohs(udp->source);
        event->dst_port = bpf_ntohs(udp->dest);
    }
    
    // Check against policies
    // In real implementation, would iterate through policies
    // For now, we'll check if this is a drop or allow
    
    // Look up pod metadata
    struct pod_metadata *pod = bpf_map_lookup_elem(&pod_metadata_map, &event->dst_ip);
    if (pod) {
        __builtin_memcpy(event->pod_name, pod->pod_name, sizeof(event->pod_name));
        __builtin_memcpy(event->namespace, pod->namespace, sizeof(event->namespace));
    }
    
    // For demonstration, we'll check if packet is allowed
    // Real implementation would check actual policy rules
    if (event->dst_port == 80 || event->dst_port == 443) {
        event->action = ACTION_ALLOW;
        event->event_type = EVENT_POLICY_ALLOW;
    } else {
        event->action = ACTION_DROP;
        event->event_type = EVENT_POLICY_DROP;
    }
    
    bpf_ringbuf_submit(event, 0);
    
    // Return appropriate action
    if (event->action == ACTION_DROP) {
        return TC_ACT_SHOT; // Drop packet
    }
    
    return TC_ACT_OK; // Allow packet
}

// XDP hook for Cilium (operates at driver level)
SEC("xdp/policy_cilium")
int xdp_policy_cilium(struct xdp_md *ctx) {
    void *data_end = (void *)(long)ctx->data_end;
    void *data = (void *)(long)ctx->data;
    
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return XDP_PASS;
    
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return XDP_PASS;
    
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return XDP_PASS;
    
    struct policy_event *event;
    event = bpf_ringbuf_reserve(&policy_events, sizeof(*event), 0);
    if (!event)
        return XDP_PASS;
    
    event->src_ip = ip->saddr;
    event->dst_ip = ip->daddr;
    event->protocol = ip->protocol;
    event->direction = 0; // ingress for XDP
    
    // Cilium-specific policy logic would go here
    // For now, demonstrate with simple port-based rules
    
    if (ip->protocol == IPPROTO_TCP || ip->protocol == IPPROTO_UDP) {
        // Extract ports
        __u16 *ports = (void *)ip + (ip->ihl * 4);
        if ((void *)(ports + 2) > data_end) {
            bpf_ringbuf_discard(event, 0);
            return XDP_PASS;
        }
        event->src_port = bpf_ntohs(ports[0]);
        event->dst_port = bpf_ntohs(ports[1]);
    }
    
    // Cilium uses identity-based policies
    // In real implementation, would check identity maps
    event->action = ACTION_ALLOW;
    event->event_type = EVENT_POLICY_ALLOW;
    __builtin_memcpy(event->policy_name, "cilium-default", 14);
    
    bpf_ringbuf_submit(event, 0);
    
    return XDP_PASS;
}

// Kprobe for iptables (used by many CNIs including Flannel)
SEC("kprobe/nf_hook_slow")
int kprobe_nf_hook_slow(struct pt_regs *ctx) {
    struct sk_buff *skb = (struct sk_buff *)PT_REGS_PARM1(ctx);
    if (!skb)
        return 0;
    
    // Read network header
    unsigned char *head = BPF_CORE_READ(skb, head);
    unsigned char *data = BPF_CORE_READ(skb, data);
    unsigned int network_header = BPF_CORE_READ(skb, network_header);
    
    if (!head || !data)
        return 0;
    
    struct iphdr *ip = (struct iphdr *)(head + network_header);
    
    struct policy_event *event;
    event = bpf_ringbuf_reserve(&policy_events, sizeof(*event), 0);
    if (!event)
        return 0;
    
    // Safely read IP header fields
    bpf_probe_read_kernel(&event->src_ip, sizeof(event->src_ip), &ip->saddr);
    bpf_probe_read_kernel(&event->dst_ip, sizeof(event->dst_ip), &ip->daddr);
    bpf_probe_read_kernel(&event->protocol, sizeof(event->protocol), &ip->protocol);
    
    event->event_type = EVENT_POLICY_LOG;
    event->action = ACTION_LOG;
    __builtin_memcpy(event->policy_name, "iptables-trace", 14);
    
    bpf_ringbuf_submit(event, 0);
    return 0;
}

// Helper program to update pod metadata
SEC("kprobe/update_pod_metadata")
int update_pod_metadata(struct pt_regs *ctx) {
    // This would be called when CNI assigns IP to pod
    // For now, it's a placeholder
    return 0;
}

char LICENSE[] SEC("license") = "GPL";