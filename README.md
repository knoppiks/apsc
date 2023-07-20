# active-passive sidecar

A sidecar container for labeling an active-passive kubernetes workload using kubernetes lease mechanism.

## Usage

### RBAC

We need a service account with rbac rules for the container to be able to set the pod label

```yaml
apiVersion: v1
automountServiceAccountToken: true
kind: ServiceAccount
metadata:
  name: apsc-test
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: apsc
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: apsc
subjects:
- kind: ServiceAccount
  name: apsc-test
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: apsc
rules:
- apiGroups: [ "coordination.k8s.io" ]
  resources: [ "leases" ]
  verbs: [ "*" ]
- apiGroups: [ "" ]
  resources: [ "pods" ]
  verbs: [ "get", "list" ]
- apiGroups: [ "" ]
  resources: [ "pods" ]
  verbs: [ "update" ]
```

### Deployment

The container needs its pod's name and namespace to function, we can parse those using `env` with `fieldRef`s.
Furthermore, the label key can be set via `LABEL_KEY` env variable - it defaults to `apsc.knoppiks.de/state` and will be 
set to `active` on the leading pod. 

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: apsc-test
spec:
  replicas: 2
  strategy:
    type: RollingUpdate
  selector:
    matchLabels:
      app.kubernetes.io/name: apsc-test
      app.kubernetes.io/component: server
  template:
    metadata:
      labels:
        app.kubernetes.io/name: apsc-test
        app.kubernetes.io/component: server
    spec:
      automountServiceAccountToken: true
      serviceAccountName: apsc-test
      containers:
      - name: server
        image: httpd:2.4-alpine
        ports:
        - name: http
          containerPort: 80
        readinessProbe:
          httpGet:
            port: http
        livenessProbe:
          httpGet:
            port: http
      - name: apsc
        image: knoppiks/apsc
        imagePullPolicy: Always
        env:
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: LABEL_KEY
          value: "example.com/state"
```

### Utilize in service

We can now select the extra label in our service, so that traffic is only sent to the active instance.

```yaml
---
apiVersion: v1
kind: Service
metadata:
  name: apsc-test
spec:
  ports:
  - name: http
    port: 80
    targetPort: http
  selector:
    app.kubernetes.io/name: apsc-test
    app.kubernetes.io/component: server
    example.com/state: active
```
