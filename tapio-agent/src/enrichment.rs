use dashmap::DashMap;
use tapio_common::occurrence::{Context, Entity, Occurrence};

/// Metadata about a pod, extracted from K8s API.
#[derive(Debug, Clone)]
pub struct PodMeta {
    pub name: String,
    pub namespace: String,
    pub uid: String,
    pub ip: Option<String>,
    pub node: String,
    pub owner_kind: Option<String>,
    pub owner_name: Option<String>,
}

/// Thread-safe cache of pod metadata, indexed by IP and UID.
/// Populated by a K8s watcher, read by observers during enrichment.
pub struct PodCache {
    by_ip: DashMap<String, PodMeta>,
    by_uid: DashMap<String, PodMeta>,
    pub node_name: String,
    pub cluster_name: String,
}

impl PodCache {
    pub fn new(node_name: String, cluster_name: String) -> Self {
        Self {
            by_ip: DashMap::new(),
            by_uid: DashMap::new(),
            node_name,
            cluster_name,
        }
    }

    pub fn upsert(&self, meta: PodMeta) {
        if let Some(ref ip) = meta.ip {
            self.by_ip.insert(ip.clone(), meta.clone());
        }
        self.by_uid.insert(meta.uid.clone(), meta);
    }

    pub fn remove(&self, uid: &str) {
        if let Some((_, meta)) = self.by_uid.remove(uid)
            && let Some(ip) = &meta.ip
        {
            self.by_ip.remove(ip);
        }
    }

    pub fn get_by_ip(&self, ip: &str) -> Option<PodMeta> {
        self.by_ip.get(ip).map(|r| r.clone())
    }

    pub fn get_by_uid(&self, uid: &str) -> Option<PodMeta> {
        self.by_uid.get(uid).map(|r| r.clone())
    }

    pub fn pod_count(&self) -> usize {
        self.by_uid.len()
    }
}

/// Hints from the observer about how to look up the pod.
pub struct EnrichHints {
    pub src_ip: Option<String>,
    pub cgroup_path: Option<String>,
}

/// Enrich an occurrence with K8s context from the pod cache.
pub fn enrich(occ: &mut Occurrence, cache: &PodCache, hints: &EnrichHints) {
    // Try to find the pod
    let pod = hints
        .cgroup_path
        .as_deref()
        .and_then(extract_pod_uid)
        .and_then(|uid| cache.get_by_uid(&uid))
        .or_else(|| hints.src_ip.as_deref().and_then(|ip| cache.get_by_ip(ip)));

    let mut entities = Vec::new();

    if let Some(ref pod) = pod {
        entities.push(Entity {
            kind: "pod".into(),
            id: format!("{}/{}", pod.namespace, pod.name),
            name: Some(pod.name.clone()),
            version: None,
        });

        if let (Some(kind), Some(name)) = (&pod.owner_kind, &pod.owner_name) {
            entities.push(Entity {
                kind: kind.to_lowercase(),
                id: format!("{}/{}", pod.namespace, name),
                name: Some(name.clone()),
                version: None,
            });
        }
    }

    entities.push(Entity {
        kind: "node".into(),
        id: cache.node_name.clone(),
        name: Some(cache.node_name.clone()),
        version: None,
    });

    occ.context = Some(Context {
        cluster: Some(cache.cluster_name.clone()),
        node: Some(cache.node_name.clone()),
        namespace: pod.as_ref().map(|p| p.namespace.clone()),
        trace_id: None,
        span_id: None,
        entities,
        correlation_keys: vec![],
    });
}

/// Extract pod UID from a cgroup path.
/// Pattern: /kubepods/{qos}/pod{uid}/{container-id}
pub fn extract_pod_uid(cgroup_path: &str) -> Option<String> {
    for segment in cgroup_path.split('/') {
        if let Some(uid) = segment.strip_prefix("pod")
            && !uid.is_empty()
        {
            return Some(uid.to_string());
        }
    }
    None
}

/// Start watching K8s pods and populating the cache.
#[cfg(target_os = "linux")]
pub async fn watch_pods(
    client: kube::Client,
    cache: Arc<PodCache>,
    node_name: String,
) -> anyhow::Result<()> {
    use futures::TryStreamExt;
    use k8s_openapi::api::core::v1::Pod;
    use kube::{
        api::{Api, ListParams},
        runtime::watcher::{self, Event},
    };

    let pods: Api<Pod> = Api::all(client);
    let params = ListParams::default().fields(&format!("spec.nodeName={node_name}"));

    tracing::info!(node = %node_name, "watching pods");

    let stream = watcher::watcher(
        pods,
        watcher::Config::default().fields(&format!("spec.nodeName={node_name}")),
    );
    futures::pin_mut!(stream);

    while let Some(event) = stream.try_next().await? {
        match event {
            Event::Apply(pod) | Event::InitApply(pod) => {
                if let Some(meta) = extract_pod_meta(&pod) {
                    tracing::debug!(pod = %meta.name, ns = %meta.namespace, "pod cached");
                    cache.upsert(meta);
                }
            }
            Event::Delete(pod) => {
                if let Some(uid) = pod.metadata.uid.as_deref() {
                    tracing::debug!(uid = %uid, "pod removed from cache");
                    cache.remove(uid);
                }
            }
            Event::Init | Event::InitDone => {}
        }
    }

    Ok(())
}

#[cfg(target_os = "linux")]
fn extract_pod_meta(pod: &k8s_openapi::api::core::v1::Pod) -> Option<PodMeta> {
    let metadata = &pod.metadata;
    let name = metadata.name.as_deref()?;
    let namespace = metadata.namespace.as_deref().unwrap_or("default");
    let uid = metadata.uid.as_deref()?;

    let spec = pod.spec.as_ref()?;
    let node = spec.node_name.as_deref().unwrap_or("");

    let status = pod.status.as_ref();
    let ip = status.and_then(|s| s.pod_ip.clone());

    // Extract owner reference (first one — usually Deployment/ReplicaSet/DaemonSet)
    let (owner_kind, owner_name) = metadata
        .owner_references
        .as_ref()
        .and_then(|refs| refs.first())
        .map(|r| (Some(r.kind.clone()), Some(r.name.clone())))
        .unwrap_or((None, None));

    Some(PodMeta {
        name: name.to_string(),
        namespace: namespace.to_string(),
        uid: uid.to_string(),
        ip,
        node: node.to_string(),
        owner_kind,
        owner_name,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_cache() -> PodCache {
        PodCache::new("worker-1".into(), "prod".into())
    }

    fn make_pod(name: &str, ns: &str, uid: &str, ip: &str) -> PodMeta {
        PodMeta {
            name: name.into(),
            namespace: ns.into(),
            uid: uid.into(),
            ip: Some(ip.into()),
            node: "worker-1".into(),
            owner_kind: Some("Deployment".into()),
            owner_name: Some("nginx".into()),
        }
    }

    #[test]
    fn cache_lookup_by_ip() {
        let cache = make_cache();
        cache.upsert(make_pod("nginx-abc", "default", "uid-1", "10.0.0.5"));
        let pod = cache.get_by_ip("10.0.0.5").expect("should find by IP");
        assert_eq!(pod.name, "nginx-abc");
        assert_eq!(pod.namespace, "default");
    }

    #[test]
    fn cache_lookup_by_uid() {
        let cache = make_cache();
        cache.upsert(make_pod("nginx-abc", "default", "uid-1", "10.0.0.5"));
        let pod = cache.get_by_uid("uid-1").expect("should find by UID");
        assert_eq!(pod.name, "nginx-abc");
    }

    #[test]
    fn cache_remove_clears_both_indexes() {
        let cache = make_cache();
        cache.upsert(make_pod("nginx-abc", "default", "uid-1", "10.0.0.5"));
        cache.remove("uid-1");
        assert!(cache.get_by_uid("uid-1").is_none());
        assert!(cache.get_by_ip("10.0.0.5").is_none());
    }

    #[test]
    fn cache_miss_returns_none() {
        let cache = make_cache();
        assert!(cache.get_by_ip("10.0.0.99").is_none());
        assert!(cache.get_by_uid("nonexistent").is_none());
    }

    #[test]
    fn extract_pod_uid_from_cgroup() {
        assert_eq!(
            extract_pod_uid("/kubepods/burstable/podabc-123/container-456"),
            Some("abc-123".into())
        );
        assert_eq!(
            extract_pod_uid("/kubepods/besteffort/pod12345/ctr"),
            Some("12345".into())
        );
        assert_eq!(extract_pod_uid("/sys/fs/cgroup/memory"), None);
        assert_eq!(extract_pod_uid(""), None);
    }

    #[test]
    fn enrich_with_pod_context() {
        let cache = make_cache();
        cache.upsert(make_pod("nginx-abc", "default", "uid-1", "10.0.0.5"));

        let mut occ = tapio_common::Occurrence::new(
            "kernel.network.connection_refused",
            tapio_common::Severity::Warning,
            tapio_common::Outcome::Failure,
        );

        enrich(
            &mut occ,
            &cache,
            &EnrichHints {
                src_ip: Some("10.0.0.5".into()),
                cgroup_path: None,
            },
        );

        let ctx = occ.context.as_ref().expect("should have context");
        assert_eq!(ctx.cluster.as_deref(), Some("prod"));
        assert_eq!(ctx.node.as_deref(), Some("worker-1"));
        assert_eq!(ctx.namespace.as_deref(), Some("default"));
        assert!(
            ctx.entities
                .iter()
                .any(|e| e.kind == "pod" && e.name.as_deref() == Some("nginx-abc"))
        );
        assert!(
            ctx.entities
                .iter()
                .any(|e| e.kind == "deployment" && e.name.as_deref() == Some("nginx"))
        );
        assert!(ctx.entities.iter().any(|e| e.kind == "node"));
    }

    #[test]
    fn enrich_cache_miss_still_adds_node() {
        let cache = make_cache();
        let mut occ = tapio_common::Occurrence::new(
            "kernel.storage.io_error",
            tapio_common::Severity::Critical,
            tapio_common::Outcome::Failure,
        );

        enrich(
            &mut occ,
            &cache,
            &EnrichHints {
                src_ip: None,
                cgroup_path: Some("/unknown/path".into()),
            },
        );

        let ctx = occ.context.as_ref().expect("should have context");
        assert_eq!(ctx.cluster.as_deref(), Some("prod"));
        assert_eq!(ctx.node.as_deref(), Some("worker-1"));
        assert!(ctx.namespace.is_none()); // no pod found
        assert!(ctx.entities.iter().any(|e| e.kind == "node"));
    }
}
