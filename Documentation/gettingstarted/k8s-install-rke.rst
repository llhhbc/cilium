.. only:: not (epub or latex or html)

    WARNING: You are looking at unreleased Cilium documentation.
    Please use the official rendered version released here:
    https://docs.cilium.io

.. _rke_install:

********************************************
Installation Using Rancher Kubernetes Engine
********************************************

This guide walks you through installation of Cilium on `Rancher Kubernetes Engine <https://rancher.com/products/rke/>`_,
a CNCF-certified Kubernetes distribution that runs entirely within Docker containers.
RKE solves the common frustration of installation complexity with Kubernetes by
removing most host dependencies and presenting a stable path for deployment,
upgrades, and rollbacks.

Install a Cluster Using RKE
===========================

The first step is to install a cluster based on the `RKE Installation Guide <https://rancher.com/docs/rke/latest/en/installation/>`_.
When creating the cluster, make sure to `change the default network plugin <https://rancher.com/docs/rke/latest/en/config-options/add-ons/network-plugins/custom-network-plugin-example/>`_
in the config.yaml file.

Change:

.. code-block:: yaml

  network:
    options:
      flannel_backend_type: "vxlan"
    plugin: "canal"

To:

.. code-block:: yaml

  network:
    plugin: none

Deploy Cilium
=============

.. tabs::

    .. group-tab:: Installation via Helm v3

        Install Cilium via ``helm install``:

        .. parsed-literal::

           helm repo add cilium https://helm.cilium.io
           helm repo update
           helm install cilium |CHART_RELEASE| \\
              --namespace $CILIUM_NAMESPACE

    .. group-tab:: Installation via ``quick-install.yaml``

        Install Cilium via the provided ``quick-install.yaml``:

        .. parsed-literal::

            kubectl apply -f |SCM_WEB|/install/kubernetes/quick-install.yaml


    .. group-tab:: Installation via ``experimental-install.yaml``

        Install Cilium via the provided ``experimental-install.yaml``:

        .. parsed-literal::

            kubectl apply -f |SCM_WEB|/install/kubernetes/experimental-install.yaml

.. include:: k8s-install-restart-pods.rst
.. include:: k8s-install-validate.rst
.. include:: hubble-enable.rst

Now that you have a Kubernetes cluster with Cilium up and running, you can take
a couple of next steps to explore various capabilities:

* :ref:`gs_http`
* :ref:`gs_dns`
* :ref:`gs_cassandra`
* :ref:`gs_kafka`
