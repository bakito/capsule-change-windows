---
title: Change Windows
description: Schedule Kubernetes changes, protect maintenance windows, and warn users before work begins. A standalone Capsule addon with the ChangeWindow API.
businessOverview: >-
  Change Windows helps organizations plan routine IT maintenance with fewer
  surprises and less manual coordination. It gives teams advance notice and can
  prevent conflicting changes while planned work is underway. It can also pause
  development environments overnight and bring them back in the morning, helping
  control infrastructure costs. These routines run automatically on your
  Kubernetes platform, so teams can spend less time coordinating maintenance and
  more time delivering services.
---

## Reliable operations, with or without Capsule

Change Windows is being developed under the [Project Capsule](https://projectcapsule.dev/)
umbrella. Capsule is a [CNCF Sandbox project](https://www.cncf.io/projects/capsule/).
The addon grew out of a platform engineering need: making operations more reliable
for platforms serving multiple tenants. Coordinated maintenance, predictable
resource changes, and advance notice help platform teams and their tenants work
together with fewer surprises.

You can use Change Windows **with Capsule or on its own**. It selects resources
through standard Kubernetes APIs and labels; Capsule Tenant objects are not
required. The same scheduling and admission features are available in either
setup.

**No technology lock-in.** Keep your existing cloud provider, Kubernetes
distribution, and deployment tools. Capsule and Headlamp integrations are optional,
so adopting Change Windows does not tie your platform to a vendor or the rest of
the Capsule stack.
