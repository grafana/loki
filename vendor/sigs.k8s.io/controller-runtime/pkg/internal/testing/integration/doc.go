/*

Package integration implements an integration testing framework for kubernetes.

It provides components for standing up a kubernetes API, against which you can test a
kubernetes client, or other kubernetes components. The lifecycle of the components
needed to provide this API is managed by this framework.

Quickstart

Add something like the following to
your tests:

	cp := &integration.ControlPlane{}
	cp.Start()
	kubeCtl := cp.KubeCtl()
	stdout, stderr, err := kubeCtl.Run("get", "pods")
	// You can check on err, stdout & stderr and build up
	// your tests
	cp.Stop()

Components

Currently the framework provides the following components:

ControlPlane: The ControlPlane wraps Etcd & APIServer (see below) and wires
them together correctly. A ControlPlane can be stopped & started and can
provide the URL to connect to the API. The ControlPlane can also be asked for a
KubeCtl which is already correctly configured for this ControlPlane. The
ControlPlane is a good entry point for default setups.

Etcd: Manages an Etcd binary, which can be started, stopped and connected to.
By default Etcd will listen on a random port for http connections and will
create a temporary directory for its data. To configure it differently, see the
Etcd type documentation below.

APIServer: Manages an Kube-APIServer binary, which can be started, stopped and
connected to. By default APIServer will listen on a random port for http
connections and will create a temporary directory to store the (auto-generated)
certificates.  To configure it differently, see the APIServer type
documentation below.

KubeCtl: Wraps around a `kubectl` binary and can `Run(...)` arbitrary commands
against a kubernetes control plane.

Binaries

Etcd, APIServer & KubeCtl use the same mechanism to determine which binaries to
use when they get started.

1. If the component is configured with a `Path` the framework tries to run that
binary.
For example:

	myEtcd := &Etcd{
		Path: "/some/other/etcd",
	}
	cp := &integration.ControlPlane{
		Etcd: myEtcd,
	}
	cp.Start()

2. If the Path field on APIServer, Etcd or KubeCtl is left unset and an
environment variable named `TEST_ASSET_KUBE_APISERVER`, `TEST_ASSET_ETCD` or
`TEST_ASSET_KUBECTL` is set, its value is used as a path to the binary for the
APIServer, Etcd or KubeCtl.

3. If neither the `Path` field, nor the environment variable is set, the
framework tries to use the binaries `kube-apiserver`, `etcd` or `kubectl` in
the directory `${FRAMEWORK_DIR}/assets/bin/`.

Arguments for Etcd and APIServer

Those components will start without any configuration. However, if you want or
need to, you can override certain configuration -- one of which are the
arguments used when calling the binary.

When you choose to specify your own set of arguments, those won't be appended
to the default set of arguments, it is your responsibility to provide all the
arguments needed for the binary to start successfully.

However, the default arguments for APIServer and Etcd are exported as
`APIServerDefaultArgs` and `EtcdDefaultArgs` from this package. Treat those
variables as read-only constants. Internally we have a set of default
arguments for defaulting, the `APIServerDefaultArgs` and `EtcdDefaultArgs` are
just copies of those. So when you override them you loose access to the actual
internal default arguments, but your override won't affect the defaulting.

All arguments are interpreted as go templates. Those templates have access to
all exported fields of the `APIServer`/`Etcd` struct. It does not matter if
those fields where explicitly set up or if they were defaulted by calling the
`Start()` method, the template evaluation runs just before the binary is
executed and right after the defaulting of all the struct's fields has
happened.

	// When you want to append additional arguments ...
	etcd := &Etcd{
		// Additional custom arguments will appended to the set of default
		// arguments
		Args:    append(EtcdDefaultArgs, "--additional=arg"),
		DataDir: "/my/special/data/dir",
	}

	// When you want to use a custom set of arguments ...
	etcd := &Etcd{
		// Only custom arguments will be passed to the binary
		Args:    []string{"--one=1", "--two=2", "--three=3"},
		DataDir: "/my/special/data/dir",
	}

*/
package integration
