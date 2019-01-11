# Lift

Lift is an Alpine Linux specific light-weight alternative for
cloud-init.

<img src="doc/lift.png" width="100px" style="padding: 10px; overflow: auto;" align="left" />

In Alpine environments, one would prefer to take a lift instead of hiking, 
running or climbing to the top.

Simply make sure `lift` is run during boot. It will take a url from a passed
in kernel parameter in order to download an `alpine-data` file. This is a 
YAML file equivalent to cloud-init's user-data. Lift will download the 
alpine-data and perform the initial OS configuration. Lift will run only once, 
on first boot of the system, by default.
