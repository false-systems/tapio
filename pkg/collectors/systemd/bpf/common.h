#ifndef __SYSTEMD_BPF_COMMON_H
#define __SYSTEMD_BPF_COMMON_H

// Syscall categories for K8s operations
#define SYSCALL_NAMESPACE   1  // clone, unshare, setns
#define SYSCALL_MOUNT       2  // mount, umount  
#define SYSCALL_CGROUP      3  // cgroup operations
#define SYSCALL_NETWORK     4  // socket, iptables
#define SYSCALL_CONTAINER   5  // container runtime ops
#define SYSCALL_IMAGE       6  // image layer operations

// K8s service types
#define SERVICE_KUBELET     1
#define SERVICE_RUNTIME     2  // containerd/docker
#define SERVICE_PROXY       3  // kube-proxy
#define SERVICE_CNI         4  // calico, cilium, etc

// Event structure shared between BPF and Go
struct k8s_syscall_event {
    __u64 timestamp;
    __u32 pid;
    __u32 tgid;
    __u32 ppid;
    __u32 uid;
    __u32 gid;
    __u8  syscall_category;
    __u8  service_type;
    __u8  operation_type;
    __u8  pad;
    char  comm[16];
    char  cgroup[64];      // /system.slice/kubelet.service
    char  service_name[32]; // descriptive name
    
    union {
        // Namespace operations
        struct {
            __u32 flags;
            __u32 target_pid;
            __u32 namespace_type;
            char  namespace_path[64];
        } ns_op;
        
        // Mount operations  
        struct {
            char  source[64];
            char  target[64];
            char  fstype[16];
            __u32 flags;
        } mount_op;
        
        // Container operations
        struct {
            char  container_id[64];
            __u32 operation;
        } container_op;
        
        // Network operations
        struct {
            __u32 socket_family;
            __u32 socket_type;
            __u32 port;
            char  operation[32];
        } net_op;
        
        // Image operations
        struct {
            char  image_path[64];
            char  layer_id[64];
            __u32 operation;
        } image_op;
    } data;
};

#endif /* __SYSTEMD_BPF_COMMON_H */