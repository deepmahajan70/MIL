# Modular Interoperability Layer (MIL) with Trust-Aware Switching

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

## Overview
The **Modular Interoperability Layer (MIL)** is a research-centric IoT middleware designed to solve the dual challenges of **protocol heterogeneity** and **network security** at the edge. 

Unlike traditional gateways that perform static protocol translation, MIL utilizes a Unix-inspired resource-oriented architecture and a dynamic **Trust-Aware Switchboard**. This allows the system to mediate between disparate protocols (MQTT, CoAP, HTTP, AMQP) while actively isolating suspicious devices based on real-time behavioral analysis.

## Key Features
- **Protocol Abstraction:** Reduces $O(N^2)$ translation complexity to $O(N)$ using a Universal Message Object (UMO) intermediate representation.
- **Trust Engine:** Implements an **Asymmetric EWMA** model for device reputation tracking.
- **Adaptive Thresholding:** Dynamically adjusts security rigor based on network-wide trust variance.
- **Real-time Telemetry:** Integrated Prometheus metrics for monitoring latency, throughput, and trust trajectories.
- **Edge-Optimized:** Lean memory footprint (~130 KB state) suitable for constrained gateways.

## Mathematical Model
The system determines the routing path by comparing the device trust score ($\sigma$) against a dynamic system threshold ($\tau$):

$$\sigma_{t} = \alpha \cdot B_{t} + (1 - \alpha) \cdot \sigma_{t-1}$$

If $\sigma \ge \tau$, the packet is routed through a **Lightweight Adapter**. If $\sigma < \tau$, the packet is diverted to a **Heavy Security Adapter** for deep inspection.

## Getting Started

### Prerequisites
- Go 1.25 or higher
- Prometheus (optional, for telemetry visualization)


### Installation
```bash
git clone [https://github.com/your-username/MIL-Switchboard.git](https://github.com/your-username/MIL-Switchboard.git)
cd MIL-Switchboard
go mod init mil-switchboard
go get [github.com/prometheus/client_golang/prometheus](https://github.com/prometheus/client_golang/prometheus)

go run src/main.go
