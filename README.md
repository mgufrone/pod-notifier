# Pod Notifier Manager Controller

## Overview

The **Pod Notifier Manager Controller** is designed to keep Kubernetes cluster administrators informed about failing
pods. This lightweight controller monitors pods within the cluster and sends notifications about any failures to a
configured messaging platform.

Currently, the only supported notification platform is **Slack**, with plans to support additional platforms in the
future.

## Features

- Monitors Kubernetes pods for failures.
- Sends alerts to Slack in real-time for quick resolution.
- Designed to be lightweight and scalable.
- Easy to configure and extend.

## Installation and Setup

**Installation instructions coming soon.**

Stay tuned for a detailed guide on how to install and configure the Pod Notifier Manager Controller to fit your needs.

## Configuration

Currently, the controller supports Slack as the default messaging platform. Configuration involves setting up the
necessary Slack credentials to allow notifications via an incoming webhook.

More details and examples will be provided once installation instructions are available.

## Future Improvements

- Support for additional messaging platforms (e.g., Microsoft Teams, Discord).
- Advanced filtering and notification customization.
- Metrics and reporting for better observability.

## Contributing

Contributions are welcome! Feel free to fork the repository, make improvements, and submit a pull request.

## License

This project is licensed under the [MIT License](LICENSE).

---

Stay notified. Stay in control.