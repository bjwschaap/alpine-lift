# Lift

Lift is an Alpine Linux specific light-weight alternative for cloud-init.

<img src="doc/lift.png" width="100px" style="padding: 10px; overflow: auto;" align="left" />

In Alpine environments, one would prefer to take a lift instead of hiking,
running or climbing to the top.

Simply make sure `lift` is run during boot. It will take a url from a passed
in kernel parameter in order to download an `alpine-data` file. This is a
YAML file equivalent to cloud-init's user-data. Lift will download the
alpine-data and perform the initial OS configuration. Lift will run only once,
on first boot of the system, by default.

## Usage

In order for `lift` to bootstrap your Alpine node:

* make sure `lift` is in your image (e.g. through `apkovl`), and
* lift is started as a service during boot (provide your own openrc script)
* either pass in a url to the `alpine-data` file with the `-s` parameter to the `lift` binary;
* or pass in a url to the `alpine-data` file trough setting `alpine-data=` kernel parameter

During the boot process lift will download the `alpine-data` and configure the instance
accordingly.
