# http-max-rps

A tool for finding the max RPS of an HTTP 

`http-max-rps` will tell you the maximum rps that either an http server
or an intermediary can provide as well as the max number of concurrent
clients before performance drops. It does this using the Universal
Scalability Law.

# Flags

| Flag                 | Default                 | Description |
|----------------------|-------------------------|-------------|
|`-address`            | `http://localhost:4140` | URL of http server or intermediary |
| `-concurrencyLevels` | `1,5,10,20,30`          | levels of concurrency to test with |
| `-debug`             | `false`                 | print out some extra information for debugging |
| `-host`              | `<none>`                | value of Host header to set |
| `-timePerLevel`      | `1s`                    | how much time to spend testing each concurrency level |

Further Reading
---------------
[Coda Hale's blog post explaining the basic concepts](https://codahale.com/usl4j-and-you/)
[Baron Schwartz's excellent and clear book on the subject](https://www.vividcortex.com/resources/universal-scalability-law/)
[Neil Gunther, USL inventor, has provided supplemental material](http://www.perfdynamics.com/Manifesto/USLscalability.html)