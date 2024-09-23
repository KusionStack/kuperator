# Kuperator

[![license](https://img.shields.io/github/license/KusionStack/operating.svg)](https://github.com/KusionStack/operating/blob/main/LICENSE)
[![Operating](https://github.com/KusionStack/kuperator/actions/workflows/release.yaml/badge.svg)](https://github.com/KusionStack/kuperator/actions/workflows/release.yaml)
[![GitHub release](https://img.shields.io/github/release/KusionStack/kuperator.svg)](https://github.com/KusionStack/kuperator/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/KusionStack/kuperator)](https://goreportcard.com/report/github.com/KusionStack/kuperator)
[![codecov](https://codecov.io/gh/KusionStack/kuperator/graph/badge.svg?token=CEQX77G7UH)](https://codecov.io/gh/KusionStack/kuperator)

KusionStack Kuperator ([official site](https://www.kusionstack.io/kuperator/introduction/)) provides a set of workloads and operators
built on Kubernetes Custom Resource Definitions (CRDs), with a primary aim of bridging the gap 
between platform development and Kubernetes.

## Key features

KusionStack Kuperator currently provides the following features,
streamlining application operations when developing platforms based on Kubernetes:

* Fine-grained operation

[**PodOpsLifecycle**](https://www.kusionstack.io/kuperator/concepts/podopslifecycle) 
extends native Pod lifecycle with more phase, like `PreCheck`, `Preparing`, `PostCheck`, to allow fine-grained operation management. 

* Advanced workloads

[**CollaSet**](https://www.kusionstack.io/kuperator/manuals/collaset) designed to manage Pods with respect to PodOpsLifecycle.

[**PodDecoration**](https://www.kusionstack.io/kuperator/manuals/poddecoration) provides secondary grouping and operational capabilities for Pods.

* Streamlined pod operation

[**OperationJob**](https://www.kusionstack.io/kuperator/manuals/operationjob) controller provides scaffolding for pod operation, i.e. `Replace`.

[**ResourceConsist**](https://www.kusionstack.io/kuperator/manuals/resourceconsist) framework offers 
a graceful way to integrate resource management around Pods, like traffic control, into PodOpsLifecycle.

* Risk management

[**PodTransitionRule**](https://www.kusionstack.io/kuperator/manuals/podtransitionrule) 
is responsible to keep Pod operation risks under control.

## Getting started

### Installation

You can install Kuperator following [installation doc](https://kusionstack.io/kuperator/started/install).

### Tutorial

Please visit this [tutorial](https://kusionstack.io/kuperator/started/demo-graceful-operation) to gracefully operate an application.

Alternatively, this [video](https://www.bilibili.com/video/BV1n8411q7sP/?t=15.7) also records the e2e experience.

## Contact us
- Twitter: [KusionStack](https://twitter.com/KusionStack)
- Slack: [Kusionstack](https://join.slack.com/t/kusionstack/shared_invite/zt-19lqcc3a9-_kTNwagaT5qwBE~my5Lnxg)
- DingTalk (Chinese): 42753001
- Wechat Group (Chinese)

  <img src="docs/wx_spark.jpg" width="200" height="200"/>

## 🎖︎ Contribution guide

KusionStack Kuperator is currently in its early stages. Our goal is to simplify platform development. 
We will continue building in areas such as application operations, observability, and insight.
We welcome everyone to participate in construction with us. Visit the [Contribution Guide](docs/contributing.md) 
to understand how to participate in the contribution KusionStack project. 
If you have any questions, please [Submit the Issue](https://github.com/KusionStack/kuperator/issues).