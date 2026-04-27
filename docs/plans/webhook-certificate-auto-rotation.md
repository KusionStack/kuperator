# Webhook Certificate Auto-Rotation Design

## Background

### Problem Description

When users update kuperator via Helm, even after the controller starts normally, webhook calls fail with `TLS handshake timeout` error.

### Root Cause Analysis

1. **Missing volumeMounts in Helm Chart**

   The StatefulSet defines a volume `webhook-certs` but never mounts it:
   ```yaml
   volumes:
   - name: webhook-certs
     secret:
       secretName: webhook-certs
   # Missing: volumeMounts in container spec!
   ```

2. **Secret Name Mismatch**

   - Code creates Secret: `kusionstack-webhook-certs`
   - Helm chart references: `webhook-certs`

3. **No Certificate Expiration Detection**

   Certificates have a 1-year validity but no automatic rotation mechanism.

4. **Timing Issue**

   Certificate generation happens after webhook server tries to start, causing TLS handshake timeout.

5. **Temporary Directory Issue**

   Without volumeMount, certificates are written to a temporary directory that disappears on pod restart.

## Solution

Use `kusionstack.io/kube-utils/webhook/certmanager` package which provides:

- Automatic certificate expiration detection and rotation
- Unified Secret name configuration
- Continuous CABundle synchronization via Watch mechanism
- Non-leader-election controller (all replicas run)

## Implementation

### Files Changed

| File | Change |
|------|--------|
| `pkg/webhook/webhook.go` | Refactored to use certmanager |
| `main.go` | Updated Initialize call signature |
| `charts/templates/statefulset.yaml` | Added volumeMounts |
| `pkg/utils/pki_helpers.go` | Deleted (no longer needed) |
| `go.mod` | Added afero, golib dependencies |

### Code Changes

#### 1. `pkg/webhook/webhook.go`

Before: ~260 lines with manual certificate generation
After: ~75 lines using certmanager

```go
func Initialize(ctx context.Context, mgr manager.Manager, dnsName string) error {
    cfg := certmanager.CertConfig{
        Host:                   dnsName,
        Namespace:              getNamespace(),
        SecretName:             "webhook-certs",  // Unified name
        MutatingWebhookNames:   []string{"kusionstack-controller-manager-mutating"},
        ValidatingWebhookNames: []string{"kusionstack-controller-manager-validating"},
    }

    certMgr := certmanager.New(mgr, cfg)
    return certMgr.SetupWithManager(mgr)
}
```

#### 2. `main.go`

```go
// Before
if err := webhook.Initialize(ctx, config, dnsName, certDir); err != nil { ... }

// After
if err := webhook.Initialize(ctx, mgr, dnsName); err != nil { ... }
```

Default `cert-dir` changed from temp directory to `/webhook-certs`.

#### 3. `charts/templates/statefulset.yaml`

```yaml
containers:
- name: manager
  volumeMounts:
  - name: webhook-certs
    mountPath: /webhook-certs
    readOnly: false  # Need write permission for cert rotation
```

## Certificate Workflow

### Startup Sequence

```
1. ctrl.NewManager()
   └── webhookServer created with CertDir="/webhook-certs"

2. webhook.Initialize(ctx, mgr, dnsName)
   └── certmanager.SetupWithManager
       ├── FSCertProvider initialized (path="/webhook-certs")
       ├── SecretCertProvider initialized (secret="webhook-certs")
       └── Manual Reconcile called
           ├── Load/Generate certs from Secret
           ├── Write certs to filesystem
           │   ├── tls.key (server private key)
           │   ├── tls.crt (server certificate)
           │   ├── ca.key (CA private key)
           │   └── ca.crt (CA certificate)
           └── Update CABundle in WebhookConfigurations

3. mgr.Start(ctx)
   └── webhook.Server.Start()
       ├── certwatcher reads tls.key + tls.crt
       ├── Starts fsnotify watcher for file changes
       └── TLS server ready
```

### Certificate Auto-Rotation

```
Secret/WebhookConfiguration change triggers Reconcile:

1. SecretCertProvider.Ensure()
   ├── Load existing certs from Secret
   └── Validate(host) - check expiration and DNSName
       └── If invalid: GenerateSelfSignedCerts()

2. FSCertProvider.Overwrite()
   └── Write new certs to filesystem
   └── fsnotify detects file change

3. certwatcher.handleEvent()
   └── ReadCertificate() - reload certs
   └── Update currentCert

4. New TLS connections automatically use new certificate
   (no server restart needed)
```

## Certificate Files Explanation

| File | Purpose | Location |
|------|---------|----------|
| `ca.key` | CA private key for signing certificates | Secret, filesystem (not used directly) |
| `ca.crt` | CA certificate = CABundle for verification | Secret, WebhookConfiguration.CABundle |
| `tls.key` | Server private key for TLS encryption | Secret, filesystem (webhook server) |
| `tls.crt` | Server certificate for TLS identity | Secret, filesystem (webhook server) |

### TLS Handshake Flow

```
API Server (Client)           Webhook Server
─────────────────             ────────────────

1. Connect to port 9443 ────────────────────────▶

2. ◀─── Send tls.crt (server certificate)

3. Verify using CABundle (ca.crt) ───────────────▶
   ✓ Signature valid
   ✓ Not expired
   ✓ DNSName matches

4. Key exchange with tls.crt's public key ───────▶

5. ◀─── Decrypt with tls.key, establish encrypted channel

6. Send AdmissionReview (encrypted) ────────────▶

7. ◀─── Response (encrypted)
```

## Comparison

| Feature | Old Approach | New Approach (certmanager) |
|---------|-------------|---------------------------|
| Certificate expiration detection | ❌ None | ✅ Validate() auto-detects |
| Auto rotation | ❌ Manual delete Secret | ✅ Expired → auto regenerate |
| Secret name consistency | ❌ Mismatch | ✅ Unified to `webhook-certs` |
| volumeMounts | ❌ Missing | ✅ Added |
| CABundle sync | Only at startup | ✅ Watch + continuous sync |
| TLS handshake | May timeout | ✅ Certs ready before server starts |
| Multi-replica support | Leader election exclusive | ✅ All replicas run (NeedLeaderElection=false) |

## Dependencies Added

- `github.com/spf13/afero v1.11.0` - filesystem abstraction
- `github.com/zoumo/golib v0.2.0` - certificate utilities
- `kusionstack.io/kube-utils/cert` - cert provider package
- `kusionstack.io/kube-utils/webhook/certmanager` - cert manager controller

## Testing

All webhook tests pass:
```
ok  	kusionstack.io/kuperator/pkg/webhook/server/generic/collaset
ok  	kusionstack.io/kuperator/pkg/webhook/server/generic/pod/gracedelete
ok  	kusionstack.io/kuperator/pkg/webhook/server/generic/pod/opslifecycle
ok  	kusionstack.io/kuperator/pkg/webhook/server/generic/poddecoration
ok  	kusionstack.io/kuperator/pkg/webhook/server/generic/podtransitionrule
```

Build successful: `go build ./...`