<p align="center"><img src="https://github.com/jetstack/cert-manager/blob/master/logo/logo.png" width="250x" /></p>
</a>
<a href="https://godoc.org/github.com/cert-manager/approver-policy"><img src="https://godoc.org/github.com/cert-manager/approver-policy?status.svg"></a>
<a href="https://goreportcard.com/report/github.com/cert-manager/approver-policy"><img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/cert-manager/approver-policy" /></a></p>

# policy-approver

policy-approver is a [cert-manager](https://cert-manager.io) approver that is responsible for
[Approving or Denying CertificateRequests](https://cert-manager.io/docs/concepts/certificaterequest/#approval).

policy-approver exposes the CertificateRequestPolicy resource which
administrators use to define policy over what, who, and how certificates are
signed by cert-manager.

---


## Installation

[cert-manager](https://cert-manager.io) is required to be installed with policy-approver.

> :warning:
>
> It is important that the
> [default approver is disabled in cert-manager](https://cert-manager.io/docs/concepts/certificaterequest/#approver-controller).
> If the default approver is not disabled in cert-manager, policy-approver will
> race with cert-manager and thus policy becomes useless.
>
> ```terminal
> $ helm upgrade -i -n cert-manager cert-manager jetstack/cert-manager --set extraArgs={--controllers='*\,-certificaterequests-approver'} --set installCRDs=true --create-namespace
> ```
>
> :warning:

To install policy-approver:

```terminal
$ helm repo add jetstack https://charts.jetstack.io --force-update
$ helm upgrade -i -n cert-manager cert-manager-policy-approver jetstack/cert-manager-policy-approver --wait
```

If you are using policy-approver with [external
issuers](https://cert-manager.io/docs/configuration/external/), you _must_
include their signer names so that policy-approver has permissions to approve
and deny CertificateRequests that
[reference them](https://cert-manager.io/docs/concepts/certificaterequest/#rbac-syntax).
For example, if using policy-approver for the internal issuer types, along with
[google-ca-issuer](https://github.com/jetstack/google-cas-issuer), and
[aws-privateca-issuer](https://github.com/cert-manager/aws-privateca-issuer),
set the following values when installing:

```terminal
$ helm upgrade -i -n cert-manager cert-manager-policy-approver jetstack/cert-manager-policy-approver --wait \
  --set app.approveSignerNames="{\
issuers.cert-manager.io/*,clusterissuers.cert-manager.io/*,\
googlecasclusterissuers.cas-issuer.jetstack.io/*,googlecasissuers.cas-issuer.jetstack.io/*,\
awspcaclusterissuers.awspca.cert-manager.io/*,awspcaissuers.awspca.cert-manager.io/*\
}"
```

---

## Configuration

> Example policy resources can be found [here](./docs/examples/).

When a CertificateRequest is created, policy-approver will evaluate whether the
request is appropriate for any existing policy, and if so, evaluate whether it
should be approved or denied.

For a CertificateRequest to be appropriate for a policy and therefore be
evaluated by it, it must be both bound via RBAC _and_ be selected by the policy
selector. CertificateRequestPolicy currently only supports `issuerRef` as a
selector.

**If at least one policy permits the request, the request is approved. If at
least one policy is appropriate for the request but none of those permit the
request, the request is denied.**

CertificateRequestPolicies are cluster scoped resources that can be thought of
as "policy profiles". They describe any request that is approved by that
policy. Policies are bound to Kubernetes users and ServiceAccounts using RBAC.

Below is an example of a policy that is bound to all Kubernetes users who may
only request certificates that have the common name of "hello.world".

```yaml
apiVersion: policy.cert-manager.io/v1alpha1
kind: CertificateRequestPolicy
metadata:
  name: test-policy
spec:
  allowed:
    commonName: "hello.world"
  selector:
    # Select all IssuerRef
    issuerRef: {}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cert-manager-policy:hello-world
rules:
  - apiGroups: ["policy.cert-manager.io"]
    resources: ["certificaterequestpolicies"]
    verbs: ["use"]
    # Name of the CertificateRequestPolicies to be used.
    resourceNames: ["test-policy"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: cert-manager-policy:hello-world
roleRef:
# ClusterRole or Role _must_ be bound to a user for the policy to be considered.
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cert-manager-policy:hello-world
subjects:
# The users who should be bound to the policies defined.
# Note that in the case of users creating Certificate resources, cert-manager
# is the entity that is creating the actual CertificateRequests, and so the
# cert-manager controller's
# Service Account should be bound instead.
- kind: Group
  name: system:authenticated
  apiGroup: rbac.authorization.k8s.io
```

---

## Behaviour

CertificateRequestPolicy are split into 4 parts; `allowed`, `contraints`,
`selector`, and `plugins`.

### Allowed

Allowed is the block that defines attributes that match against the
corresponding attribute in the request. A request is permitted by the policy if
the request omits an allowed attribute, but will _deny_ the request if it
contains an attribute which is _not_ present in the allowed block.

In the following CertificateRequestPolicy, a request will be permitted if it
does not request a DNS name, requests the DNS name "example.com", but will
be denied when requesting "bar.example.com".

```yaml
spec:
  ...
  allowed:
    dnsNames:
    - "example.com"
    - "foo.example.com"
  ...
```

If an allowed field is omitted, that attribute is considered "deny all" for
requests.

Allowed string fields accept wildcards "\*" within its values. Wildcards "\*" in
patterns represent any string that has a length of 0 or more. A pattern
containing only "\*" will match anything. A pattern containing `"\*foo"` will
match `"foo"` as well as any string which ends in `"foo"` (e.g. `"bar-foo"`). A
pattern containing `"\*.foo"` will match `"bar-123.foo"`, but not `"barfoo"`.

Allowed fields that are lists will permit requests that are a subset of that
list. This means that if `usages` contains `["server auth", "client auth"]`,
then a request containing only `["server auth"]` would be permitted, but not
`["server auth", "cert sign"]`.

Below is an example including all supported allowed fields of
CertificateRequestPolicy.

```yaml
apiVersion: policy.cert-manager.io/v1alpha1
kind: CertificateRequestPolicy
metadata:
  name: my-policy
spec:
  allowed:
    commonName: "example.com"
    dnsNames:
    - "example.com"
    - "*.example.com"
    ipAddresses:
    - "1.2.3.4"
    - "10.0.1.*"
    uris:
    - "spiffe://example.org/ns/*/sa/*"
    emailAddresses:
    - "*@example.com"
    isCA: false
    usages:
    - "server auth"
    - "client auth"
    subject:
      organizations: ["hello-world"]
      countries: ["*"]
      organizationalUnits: ["*"]
      localities: ["*"]
      provinces: ["*"]
      streetAddresses: ["*"]
      postalCodes: ["*"]
      serialNumber: "*"
  ...
```

### Constraints

Constraints is the block that is used to limit what attributes the request can
have. If a constraint is not defined, then the attribute is considered "allow
all".

Below is an example containing all supported constraints fields of
CertificateRequestPolicy.

```yaml
apiVersion: policy.cert-manager.io/v1alpha1
kind: CertificateRequestPolicy
metadata:
  name: my-policy
spec:
  ...
  constraints:
    minDuration: 1h
    maxDuration: 24h
    privateKey:
      algorithm: RSA
      minSize: 2048
      maxSize: 4096
  ...
```

### Selector

Selector is a required field that is used for matching
CertificateRequestPolicies against a CertificateRequest for evaluation.
policy-approver currently only supports selecting over the `issuerRef` of a
request.

`issuerRef` values accept wildcards "\*". If an `issuerRef` is set to an empty
object "{}", then the policy will match against _all_ RBAC bound requests.

```yaml
apiVersion: policy.cert-manager.io/v1alpha1
kind: CertificateRequestPolicy
metadata:
  name: my-policy
spec:
  ...
  selector:
    issuerRef:
    - name: "my-ca"
      kind: "*Issuer"
      group: "cert-manager.io"
```

```yaml
apiVersion: policy.cert-manager.io/v1alpha1
kind: CertificateRequestPolicy
metadata:
  name: match-all-requests
spec:
  ...
  selector:
    issuerRef: {}
```

> :warning: Note that the user must still be bound by [RBAC](#Configuration) for
> the policy to be considered for evaluation against a request.

### Plugins

Plugins are built into the policy-approver image at compile time. For more
information on plugins and how to develop them, go [here](./docs/plugins.md).

----
