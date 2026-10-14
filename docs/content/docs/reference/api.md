---
title: API reference
weight: 10
description: Generated reference for the ChangeWindow Kubernetes API.
---

Packages:

- [addons.projectcapsule.dev/v1alpha1](#addonsprojectcapsuledevv1alpha1)

# addons.projectcapsule.dev/v1alpha1

Resource Types:

- [ChangeWindow](#changewindow)




## ChangeWindow
<sup><sup>[↩ Parent](#addonsprojectcapsuledevv1alpha1 )</sup></sup>






ChangeWindow describes scheduled resource changes and admission restrictions.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
      <td><b>apiVersion</b></td>
      <td>string</td>
      <td>addons.projectcapsule.dev/v1alpha1</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b>kind</b></td>
      <td>string</td>
      <td>ChangeWindow</td>
      <td>true</td>
      </tr>
      <tr>
      <td><b><a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta">metadata</a></b></td>
      <td>object</td>
      <td>Refer to the Kubernetes API documentation for the fields of the `metadata` field.</td>
      <td>true</td>
      </tr><tr>
        <td><b><a href="#changewindowspec">spec</a></b></td>
        <td>object</td>
        <td>
          ChangeWindowSpec selects namespaces and resources for scheduled behavior.<br/>
          <br/>
            <i>Validations</i>:<li>!has(self.actions) || size(self.actions) == 0 || has(self.serviceAccount): actions require a serviceAccount</li>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#changewindowstatus">status</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec
<sup><sup>[↩ Parent](#changewindow)</sup></sup>



ChangeWindowSpec selects namespaces and resources for scheduled behavior.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#changewindowspecschedulesindex">schedules</a></b></td>
        <td>[]object</td>
        <td>
          Schedules form a union of overlapping and adjacent windows.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#changewindowspectargetsindex">targets</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#changewindowspecactionsindex">actions</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspecadmissionindex">admission</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspecconfig">config</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspecnamespaceselectorsindex">namespaceSelectors</a></b></td>
        <td>[]object</td>
        <td>
          NamespaceSelectors are ORed for namespaced targets. Required when any target is namespaced.
Omit for Node-only windows; Node targets use their own label selector.
An explicit empty selector matches all non-protected namespaces.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspecserviceaccount">serviceAccount</a></b></td>
        <td>object</td>
        <td>
          ServiceAccount is the identity used for resource reads and SSA writes.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.schedules[index]
<sup><sup>[↩ Parent](#changewindowspec)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>cron</b></td>
        <td>string</td>
        <td>
          Cron is a standard five-field cron expression; seconds and TZ prefixes are not accepted.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>duration</b></td>
        <td>string</td>
        <td>
          Duration is elapsed time, greater than zero and at most 168h.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>timeZone</b></td>
        <td>string</td>
        <td>
          <br/>
          <br/>
            <i>Default</i>: UTC<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.targets[index]
<sup><sup>[↩ Parent](#changewindowspec)</sup></sup>



Target selects a resource kind and labels. Admission targets support Capsule GVK
patterns and match namespaced resources only, except concrete v1 Node targets.
Scheduled actions require a concrete namespaced apiVersion and kind.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>string</td>
        <td>
          Kind is case-sensitive and supports * patterns, for example *Set or Deploy*.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>apiVersion</b></td>
        <td>string</td>
        <td>
          APIVersion uses Capsule GVK matching: omitted/empty means core v1;
apps matches any apps version, apps/v1 is exact, and * matches all groups
and versions. Partial patterns such as apps/* and *.example.com/v1 work too.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspectargetsindexselector">selector</a></b></td>
        <td>object</td>
        <td>
          Selector selects resource labels. For Nodes it selects nodes directly;
for namespaced targets it applies within the selected namespaces.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.targets[index].selector
<sup><sup>[↩ Parent](#changewindowspectargetsindex)</sup></sup>



Selector selects resource labels. For Nodes it selects nodes directly;
for namespaced targets it applies within the selected namespaces.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#changewindowspectargetsindexselectormatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
map is equivalent to an element of matchExpressions, whose key field is "key", the
operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.targets[index].selector.matchExpressions[index]
<sup><sup>[↩ Parent](#changewindowspectargetsindexselector)</sup></sup>



A label selector requirement is a selector that contains values, a key, and an operator that
relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values.
Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn,
the values array must be non-empty. If the operator is Exists or DoesNotExist,
the values array must be empty. This array is replaced during a strategic
merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.actions[index]
<sup><sup>[↩ Parent](#changewindowspec)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>targets</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#changewindowspecactionsindexcapturekey">capture</a></b></td>
        <td>map[string]object</td>
        <td>
          Capture stores only explicitly selected JSON pointer values before any mutation.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspecactionsindexonend">onEnd</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspecactionsindexonstart">onStart</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.actions[index].capture[key]
<sup><sup>[↩ Parent](#changewindowspecactionsindex)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>path</b></td>
        <td>string</td>
        <td>
          Path is an RFC 6901 JSON pointer.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>subresource</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.actions[index].onEnd
<sup><sup>[↩ Parent](#changewindowspecactionsindex)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#changewindowspecactionsindexonendapply">apply</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.actions[index].onEnd.apply
<sup><sup>[↩ Parent](#changewindowspecactionsindexonend)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>template</b></td>
        <td>string</td>
        <td>
          Template renders a partial YAML/JSON object. Identity is supplied by the controller.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>force</b></td>
        <td>boolean</td>
        <td>
          Force explicitly transfers ownership of conflicting fields.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>subresource</b></td>
        <td>string</td>
        <td>
          Subresource is an actual Kubernetes subresource, e.g. scale (not rollout).<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.actions[index].onStart
<sup><sup>[↩ Parent](#changewindowspecactionsindex)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#changewindowspecactionsindexonstartapply">apply</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.actions[index].onStart.apply
<sup><sup>[↩ Parent](#changewindowspecactionsindexonstart)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>template</b></td>
        <td>string</td>
        <td>
          Template renders a partial YAML/JSON object. Identity is supplied by the controller.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>force</b></td>
        <td>boolean</td>
        <td>
          Force explicitly transfers ownership of conflicting fields.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>subresource</b></td>
        <td>string</td>
        <td>
          Subresource is an actual Kubernetes subresource, e.g. scale (not rollout).<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.admission[index]
<sup><sup>[↩ Parent](#changewindowspec)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>message</b></td>
        <td>string</td>
        <td>
          Message is a Go template with target and window context.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operations</b></td>
        <td>[]enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: CREATE, UPDATE, DELETE<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>targets</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>action</b></td>
        <td>enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: Deny<br/>
            <i>Default</i>: Deny<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspecadmissionindexaudience">audience</a></b></td>
        <td>object</td>
        <td>
          Audience selects authenticated request identities. Omitted or empty means everyone.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspecadmissionindexmatchconditionsindex">matchConditions</a></b></td>
        <td>[]object</td>
        <td>
          MatchConditions are ANDed CEL expressions. An empty list matches all requests.
A false result skips this entry. Evaluation errors fail closed for denial rules;
warning errors return a diagnostic warning without denying the request.
Variables: object, oldObject, request, namespaceObject. Objects are the actual
admission payloads (e.g. Scale for /scale); absent objects are null.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>subresources</b></td>
        <td>[]string</td>
        <td>
          Subresources defaults to the parent resource only. Use "scale" for /scale,
"*" for all subresources, and ["", "*"] for parent and subresource writes.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>when</b></td>
        <td>enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: Active, Inactive<br/>
            <i>Default</i>: Active<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.admission[index].audience
<sup><sup>[↩ Parent](#changewindowspecadmissionindex)</sup></sup>



Audience selects authenticated request identities. Omitted or empty means everyone.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#changewindowspecadmissionindexaudienceexcludeindex">exclude</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspecadmissionindexaudienceincludeindex">include</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.admission[index].audience.exclude[index]
<sup><sup>[↩ Parent](#changewindowspecadmissionindexaudience)</sup></sup>



Subject identifies an authenticated user, group, or namespaced service account.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: User, Group, ServiceAccount<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          Namespace is mandatory for ServiceAccounts and forbidden for Users and Groups.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.admission[index].audience.include[index]
<sup><sup>[↩ Parent](#changewindowspecadmissionindexaudience)</sup></sup>



Subject identifies an authenticated user, group, or namespaced service account.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: User, Group, ServiceAccount<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          Namespace is mandatory for ServiceAccounts and forbidden for Users and Groups.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.admission[index].matchConditions[index]
<sup><sup>[↩ Parent](#changewindowspecadmissionindex)</sup></sup>



MatchCondition is a named CEL boolean expression compiled with the policy.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>expression</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.config
<sup><sup>[↩ Parent](#changewindowspec)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#changewindowspecconfigadmission">admission</a></b></td>
        <td>object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.config.admission
<sup><sup>[↩ Parent](#changewindowspecconfig)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#changewindowspecconfigadmissionwarningsindex">warnings</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.config.admission.warnings[index]
<sup><sup>[↩ Parent](#changewindowspecconfigadmission)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>message</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operations</b></td>
        <td>[]enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: CREATE, UPDATE, DELETE<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>targets</b></td>
        <td>[]string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>when</b></td>
        <td>enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: BeforeStart, Active, BeforeEnd<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b><a href="#changewindowspecconfigadmissionwarningsindexaudience">audience</a></b></td>
        <td>object</td>
        <td>
          Audience selects authenticated request identities. Omitted or empty means everyone.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspecconfigadmissionwarningsindexmatchconditionsindex">matchConditions</a></b></td>
        <td>[]object</td>
        <td>
          MatchConditions are ANDed CEL expressions. An empty list matches all requests.
A false result skips this entry. Evaluation errors fail closed for denial rules;
warning errors return a diagnostic warning without denying the request.
Variables: object, oldObject, request, namespaceObject. Objects are the actual
admission payloads (e.g. Scale for /scale); absent objects are null.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>subresources</b></td>
        <td>[]string</td>
        <td>
          Subresources defaults to the parent resource only. Use "scale" for /scale,
"*" for all subresources, and ["", "*"] for parent and subresource writes.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>within</b></td>
        <td>string</td>
        <td>
          Within is required for BeforeStart/BeforeEnd and must be positive, at most 168h.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.config.admission.warnings[index].audience
<sup><sup>[↩ Parent](#changewindowspecconfigadmissionwarningsindex)</sup></sup>



Audience selects authenticated request identities. Omitted or empty means everyone.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#changewindowspecconfigadmissionwarningsindexaudienceexcludeindex">exclude</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowspecconfigadmissionwarningsindexaudienceincludeindex">include</a></b></td>
        <td>[]object</td>
        <td>
          <br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.config.admission.warnings[index].audience.exclude[index]
<sup><sup>[↩ Parent](#changewindowspecconfigadmissionwarningsindexaudience)</sup></sup>



Subject identifies an authenticated user, group, or namespaced service account.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: User, Group, ServiceAccount<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          Namespace is mandatory for ServiceAccounts and forbidden for Users and Groups.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.config.admission.warnings[index].audience.include[index]
<sup><sup>[↩ Parent](#changewindowspecconfigadmissionwarningsindexaudience)</sup></sup>



Subject identifies an authenticated user, group, or namespaced service account.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>kind</b></td>
        <td>enum</td>
        <td>
          <br/>
          <br/>
            <i>Enum</i>: User, Group, ServiceAccount<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          Namespace is mandatory for ServiceAccounts and forbidden for Users and Groups.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.config.admission.warnings[index].matchConditions[index]
<sup><sup>[↩ Parent](#changewindowspecconfigadmissionwarningsindex)</sup></sup>



MatchCondition is a named CEL boolean expression compiled with the policy.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>expression</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.namespaceSelectors[index]
<sup><sup>[↩ Parent](#changewindowspec)</sup></sup>



A label selector is a label query over a set of resources. The result of matchLabels and
matchExpressions are ANDed. An empty label selector matches all objects. A null
label selector matches no objects.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b><a href="#changewindowspecnamespaceselectorsindexmatchexpressionsindex">matchExpressions</a></b></td>
        <td>[]object</td>
        <td>
          matchExpressions is a list of label selector requirements. The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>matchLabels</b></td>
        <td>map[string]string</td>
        <td>
          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
map is equivalent to an element of matchExpressions, whose key field is "key", the
operator is "In", and the values array contains only "value". The requirements are ANDed.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.namespaceSelectors[index].matchExpressions[index]
<sup><sup>[↩ Parent](#changewindowspecnamespaceselectorsindex)</sup></sup>



A label selector requirement is a selector that contains values, a key, and an operator that
relates the key and values.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>key</b></td>
        <td>string</td>
        <td>
          key is the label key that the selector applies to.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>operator</b></td>
        <td>string</td>
        <td>
          operator represents a key's relationship to a set of values.
Valid operators are In, NotIn, Exists and DoesNotExist.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>values</b></td>
        <td>[]string</td>
        <td>
          values is an array of string values. If the operator is In or NotIn,
the values array must be non-empty. If the operator is Exists or DoesNotExist,
the values array must be empty. This array is replaced during a strategic
merge patch.<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.spec.serviceAccount
<sup><sup>[↩ Parent](#changewindowspec)</sup></sup>



ServiceAccount is the identity used for resource reads and SSA writes.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>name</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>namespace</b></td>
        <td>string</td>
        <td>
          <br/>
        </td>
        <td>true</td>
      </tr></tbody>
</table>


### ChangeWindow.status
<sup><sup>[↩ Parent](#changewindow)</sup></sup>





<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>active</b></td>
        <td>boolean</td>
        <td>
          Active mirrors the Active condition for compatibility. Conditions distinguish unknown state.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b><a href="#changewindowstatusconditionsindex">conditions</a></b></td>
        <td>[]object</td>
        <td>
          Conditions report reconciliation readiness (Ready) and schedule occupancy (Active).
Active reasons are Planned before the first observed window, Active during a window,
Finishing while restoration is outstanding, and Finished after an observed window completes.
Finished persists until another window starts. Unknown denotes invalid policy;
Unscheduled means no occurrence is available and none has been observed.<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>nextTransition</b></td>
        <td>string</td>
        <td>
          NextTransition is the active window's end or the next planned start, including after Finished.<br/>
          <br/>
            <i>Format</i>: date-time<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>observedGeneration</b></td>
        <td>integer</td>
        <td>
          <br/>
          <br/>
            <i>Format</i>: int64<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>pendingResources</b></td>
        <td>integer</td>
        <td>
          PendingResources counts journal entries still requiring completion.<br/>
          <br/>
            <i>Format</i>: int32<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>windowEnd</b></td>
        <td>string</td>
        <td>
          WindowEnd is absent when the effective end is beyond the bounded scheduling horizon.<br/>
          <br/>
            <i>Format</i>: date-time<br/>
        </td>
        <td>false</td>
      </tr><tr>
        <td><b>windowStart</b></td>
        <td>string</td>
        <td>
          <br/>
          <br/>
            <i>Format</i>: date-time<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>


### ChangeWindow.status.conditions[index]
<sup><sup>[↩ Parent](#changewindowstatus)</sup></sup>



Condition contains details for one aspect of the current state of this API Resource.

<table>
    <thead>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Required</th>
        </tr>
    </thead>
    <tbody><tr>
        <td><b>lastTransitionTime</b></td>
        <td>string</td>
        <td>
          lastTransitionTime is the last time the condition transitioned from one status to another.
This should be when the underlying condition changed.  If that is not known, then using the time when the API field changed is acceptable.<br/>
          <br/>
            <i>Format</i>: date-time<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>message</b></td>
        <td>string</td>
        <td>
          message is a human readable message indicating details about the transition.
This may be an empty string.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>reason</b></td>
        <td>string</td>
        <td>
          reason contains a programmatic identifier indicating the reason for the condition's last transition.
Producers of specific condition types may define expected values and meanings for this field,
and whether the values are considered a guaranteed API.
The value should be a CamelCase string.
This field may not be empty.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>status</b></td>
        <td>enum</td>
        <td>
          status of the condition, one of True, False, Unknown.<br/>
          <br/>
            <i>Enum</i>: True, False, Unknown<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>type</b></td>
        <td>string</td>
        <td>
          type of condition in CamelCase or in foo.example.com/CamelCase.<br/>
        </td>
        <td>true</td>
      </tr><tr>
        <td><b>observedGeneration</b></td>
        <td>integer</td>
        <td>
          observedGeneration represents the .metadata.generation that the condition was set based upon.
For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9, the condition is out of date
with respect to the current state of the instance.<br/>
          <br/>
            <i>Format</i>: int64<br/>
            <i>Minimum</i>: 0<br/>
        </td>
        <td>false</td>
      </tr></tbody>
</table>
