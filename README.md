# 🌲 Tapio - The Forest Guardian for Kubernetes

**Making Kubernetes and eBPF accessible to everyone**

---

## What is Tapio?

**Tapio makes Kubernetes debugging simple.**

Instead of this:
```bash
kubectl describe pod my-app
kubectl logs my-app --previous  
kubectl top pod my-app
# ... 10 more commands to understand what's wrong
```

You get this:
```bash
tapio check my-app
⚠️  my-app will crash in 8 minutes - memory leak detected
🔧 Fix it: tapio fix my-app --memory
```

**One command. Instant understanding. Immediate action.**

---

## Why "Tapio"?

In Finnish mythology, **Tapio** is the god of forests who protects trees and maintains balance.

**Your Kubernetes cluster is a digital forest:**
- **Pods** are trees that need protection
- **Tapio** watches over them and keeps your cluster healthy

**"A healthy forest needs a wise guardian."** 🌲⚡

---

## Core Commands

```bash
tapio check                    # "Is my cluster healthy?"
tapio fix                      # "Fix problems automatically"  
tapio why my-app              # "Why is this broken?"
tapio watch                   # "Alert me when issues appear"
```

---

## Quick Start

```bash
# Install (coming soon)
curl -sfL https://get.tapio.sh | sh

# Use immediately
tapio check
✅ 5 pods healthy
⚠️  1 pod will OOM in 3 minutes
🔧 Fix: tapio fix api-service --memory
```

**No configuration. No setup. Just works.**

---

## The Magic

- **Kubernetes API**: What your cluster thinks is happening
- **eBPF Kernel Data**: What's actually happening  
- **AI Correlation**: Predicts problems before they happen

**Result**: Fix issues before your users notice them.

---

## Status

🚧 **Under Active Development**

We're building the future of Kubernetes debugging. Simple, powerful, accessible to everyone.

**Coming Soon**: Alpha release with basic health checking

---

*"Every tree in the digital forest deserves protection."* 🌲
