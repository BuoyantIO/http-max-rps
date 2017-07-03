package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"gonum.org/v1/gonum/matrix/mat64"
	"gonum.org/v1/gonum/optimize"
)

// `http-max-rps` is designed to tell you the maximum rps that
// either an http server or an intermediary can provide. It does
// this using the Universal Scalability Law.
//
// Thanks to @brendantracey for the go playground snippet least squared regression
// code that I borrowed verbatim.
func main() {
	var (
		address           = flag.String("address", "http://localhost:4140", "URL of http server or intermediary")
		host 			  = flag.String("host", "", "value of Host header to set")
		concurrencyLevels = flag.String("concurrencyLevels", "1,5,10,20,30", "levels of concurrency to test with")
		timePerLevel      = flag.Duration("timePerLevel", 1 * time.Second, "how much time to spend testing each concurrency level")
		debug             = flag.Bool("debug", false, "print out some extra information for debugging")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n", path.Base(os.Args[0]))
		flag.PrintDefaults()
	}

	flag.Parse()

	if *timePerLevel < time.Second {
		log.Fatalf("timePerLevel cannot be less than 1 second.")
	}

	levels := strings.Split(*concurrencyLevels, ",")
	var denseLatency [](float64)

	for _, l := range levels {
		level, err := strconv.Atoi(l)
		if err != nil {
			log.Fatalf("unknown concurrency level: %s, %s", l, err)
		}

		throughput := runLoadTests(address, host, level, timePerLevel)
		if *debug {
			fmt.Printf("%d %d\n", level, throughput)
		}
		denseLatency = append(denseLatency, float64(level))
		denseLatency = append(denseLatency, float64(throughput))
	}

	latency := mat64.NewDense(len(denseLatency) / 2, 2, denseLatency)
	concurrency := mat64.Col(nil, 0, latency)
	throughput := mat64.Col(nil, 1, latency)

	// `f` and `grad` were borrowed from https://play.golang.org/p/wWUH4E5LhP
	f := func(x []float64) float64 {
		sigma, kappa, lambda := optvarsToGreek(x)
		var mismatch float64
		for i, N := range concurrency {
			pred := concurrencyToThroughput(N, sigma, kappa, lambda)
			truth := throughput[i]
			mismatch += (pred - truth) * (pred - truth)
		}
		return mismatch
	}

	grad := func(grad, x []float64) {
		for i := range grad {
			grad[i] = 0
		}
		sigma, kappa, lambda := optvarsToGreek(x)
		dSigmaDX, dKappaDX, dLambdaDX := optvarsToGreekDeriv(x)
		for i, N := range concurrency {
			pred := concurrencyToThroughput(N, sigma, kappa, lambda)
			truth := throughput[i]

			dMismatchDPred := 2 * (pred - truth)
			dPredDSigma, dPredDKappa, dPredDLambda := concurrencyToThroughputDeriv(N, sigma, kappa, lambda)

			grad[0] += dMismatchDPred * dPredDSigma * dSigmaDX
			grad[1] += dMismatchDPred * dPredDKappa * dKappaDX
			grad[2] += dMismatchDPred * dPredDLambda * dLambdaDX
		}
	}

	problem := optimize.Problem{
		Func: f,
		Grad: grad,
	}
	settings := optimize.DefaultSettings()
	settings.GradientThreshold = 1e-2 // Looser tolerance because using FD derivative

	initX := []float64{0, -1, -3} // make sure they all start positive
	result, err := optimize.Local(problem, initX, nil, nil)
	if err != nil {
		fmt.Println("Optimization error:", err)
	}

	sigmaOpt, kappaOpt, lambdaOpt := optvarsToGreek(result.X)
	fmt.Println("sigma (the overhead of contention): ", sigmaOpt)
	fmt.Println("kappa (the overhead of crosstalk): ", kappaOpt)
	fmt.Println("lambda (unloaded performance): ", lambdaOpt)

	if *debug {
		for i, v := range throughput {
			N := concurrency[i]
			pred := concurrencyToThroughput(N, sigmaOpt, kappaOpt, lambdaOpt)
			fmt.Println("true", v, "pred", pred)
		}
	}

	maxConcurrency := math.Floor(math.Sqrt((1 - sigmaOpt) / kappaOpt))
	fmt.Printf("maxConcurrency: %f\n", maxConcurrency)

	maxRps := throughputAtConcurrency(float64(maxConcurrency), kappaOpt, lambdaOpt, sigmaOpt)
	fmt.Printf("maxRps: %f\n", maxRps)
}

func exUsage(msg string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, fmt.Sprintf(msg, args...))
	fmt.Fprintln(os.Stderr, "Try --help for help.")
	os.Exit(64)
}

func throughputAtConcurrency(n, kappa, lambda, sigma float64) float64 {
    return (lambda * n) / (1 + (sigma * (n - 1)) + (kappa * n * (n - 1)));
}

// These math functions were borrowed from https://play.golang.org/p/wWUH4E5LhP
func optvarsToGreek(x []float64) (sigma, kappa, lambda float64) {
	return math.Exp(x[0]), math.Exp(x[1]), math.Exp(x[2])
}

func optvarsToGreekDeriv(x []float64) (dSigmaDX, dKappaDX, dLambdaDX float64) {
	return math.Exp(x[0]), math.Exp(x[1]), math.Exp(x[2])
}

func concurrencyToThroughput(concurrency, sigma, kappa, lambda float64) float64 {
	N := concurrency
	return lambda * N / (1 + sigma*(N-1) + kappa*N*(N-1))
}

func concurrencyToThroughputDeriv(concurrency, sigma, kappa, lambda float64) (dSigma, dKappa, dLambda float64) {
	// X(N) = lambda * N / (1 + sigma*(N-1) + kappa*N*(N-1))
	N := concurrency
	num := lambda * N
	denom := 1 + sigma*(N-1) + kappa*N*(N-1)
	dSigma = -(num / (denom * denom)) * (N - 1)
	dKappa = -(num / (denom * denom)) * (N - 1) * N
	dLambda = N / denom
	return dSigma, dKappa, dLambda
}

// Converts a slice of chan int to a slice of int.
func chansToSlice(cs []<-chan int, size int) []int {
    s := make([]int, size)
	for i, c := range cs {
		for m := range c {
			s[i] = m
	    }
	}
    return s
}

func newClient(
	compress bool,
	https bool,
	noreuse bool,
	maxConn int,
) *http.Client {
	tr := http.Transport{
		DisableCompression:  !compress,
		DisableKeepAlives:   noreuse,
		MaxIdleConnsPerHost: maxConn,
		Proxy:               http.ProxyFromEnvironment,
		Dial: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	if https {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &tr,
	}
}

func sendRequest(
	client *http.Client,
	url *url.URL,
	host *string,
	bodyBuffer []byte,
) error {
	req, err := http.NewRequest("GET", url.String(), nil)
	req.Close = false
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		fmt.Fprintf(os.Stderr, "\n")
	}
	if *host != "" {
		req.Host = *host
	}

	response, err := client.Do(req)

	if err != nil {
		return err
	} else {
		defer response.Body.Close()
		io.CopyBuffer(ioutil.Discard, response.Body, bodyBuffer)
		return nil
	}
}

// Runs a single load test, returns how many requests were sent in a second.
func runLoadTest(client *http.Client, destURL *url.URL, host *string, wg *sync.WaitGroup, startWg *sync.WaitGroup, timePerLevel *time.Duration) <- chan int {
	out := make(chan int, 1)
	bodyBuffer := make([]byte, 50000)

	go func() {
		defer wg.Done()
		// Roughly synchronize the start of all our load test goroutines
		startWg.Wait()
		start := time.Now()
		requests := 0
		for ; time.Now().Sub(start) <= *timePerLevel; requests++ {
			err := sendRequest(client, destURL, host, bodyBuffer)

			if err != nil {
				// TODO: have an err channel so we can report the # of errs
				log.Printf("Error issuing request %v", err)
				continue
			}
		}
		rps := requests / int(timePerLevel.Seconds())
		out <- rps
		close(out)
	}()

	return out
}

// returns how many requests were sent in one second at concurrencyLevel
func runLoadTests(address *string, host *string, concurrencyLevel int, timePerLevel *time.Duration) int {
	// FIXME: wire these options through flags if needed or remove.
	client := newClient(false, false, false, concurrencyLevel)
	destURL, err := url.Parse(*address)
	if err != nil {
		exUsage("invalid URL: '%s': %s\n", address, err.Error())
	}

	var wg sync.WaitGroup
	var startWg sync.WaitGroup
	// a slice of channels containing throughput per goroutine
	var requests []<-chan int
	startWg.Add(1)
	wg.Add(concurrencyLevel)

	for i := 0; i < concurrencyLevel; i++ {
		if err != nil {
			log.Fatalf("did not connect: %v", err)
		}
		request := runLoadTest(client, destURL, host, &wg, &startWg, timePerLevel)
		requests = append(requests, request)
	}

	startWg.Done()
	wg.Wait()
	requestsPerWorker := chansToSlice(requests, concurrencyLevel)
	totalRequests := 0
	for _, requests := range requestsPerWorker {
		totalRequests += requests
	}

	return totalRequests
}
