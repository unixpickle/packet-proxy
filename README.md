# packet-proxy

A proxy for both UDP and TCP, which saves packets to a file.

For TCP connections, packets are split up using timing, i.e. bytes that come at around the same time are clustered into a "packet" on file.
