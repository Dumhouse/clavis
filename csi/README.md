# Clavis Secrets Store CSI driver

`clavis-csi` is a node-only CSI driver that writes resolved Clavis secrets as
files into a pod volume. No Kubernetes `Secret` objects are created.

## Install

```sh
kubectl create secret generic clavis-csi-token \
  --namespace clavis-system \
  --from-literal=token=clv_mt_...
kubectl apply -f deploy/csi-driver.yaml
```

Set `CLAVIS_SERVER`, `CLAVIS_ORG`, `CLAVIS_PROJECT`, and `CLAVIS_ENV` to the
defaults for this cluster. Volume attributes can override them.

## PersistentVolume

```yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: clavis-web-app-dev
spec:
  capacity:
    storage: 1Mi
  accessModes: [ReadOnlyMany]
  persistentVolumeReclaimPolicy: Retain
  csi:
    driver: clavis.csi.dumhouse.github.io
    volumeHandle: clavis-web-app-dev
    volumeAttributes:
      org: acme
      project: web-app
      environment: dev
      keys: DATABASE_URL,STRIPE_KEY
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: clavis-web-app-dev
  namespace: default
spec:
  accessModes: [ReadOnlyMany]
  resources:
    requests:
      storage: 1Mi
  volumeName: clavis-web-app-dev
```

Mount the claim in a pod and read `/etc/clavis/DATABASE_URL`. Values are written
as `0600` files and refreshed when the pod volume is republished.

## Security

- Use a scoped machine identity with `secrets:read-values` for the target
  environment only.
- The driver does not create Kubernetes Secrets, but the node can read the
  mounted files. Use `readOnly: true` and pod security policies.
- Rotating the Clavis token requires restarting the DaemonSet.
