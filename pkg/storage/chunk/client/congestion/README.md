functions:

- handle congestion
  - throttle requests before being made
  - querier-level
- handle retries
  - re-issue requests if they are retryable (server-side error + idempotent)
  - must be governed by congestion control throttler
  - request-level
- handle hedging
  - re-issue requests if they are hedgeable (server-side slowness + idempotent)
  - must be governed by congestion control throttler
  - restrict to a maximum across all workers (querier-level)
  - request-level


how do we pass this controller around:

- initialise in modules.go so it's global to process
- must satisfy client.ObjectClient interface to minimise code changes
- must only be passed to modules which need it (i.e. not analytics / compactor, just to querier)


hedging:

- we currently do not know which hedged requests succeeded or not!!
  - are we wasting a lot of money on these? are they worth it?
  - is the configuration optimal?
  - with larger numbers of queriers, max 50 hedged RPS could be a significant proportion of rate-limits (50k+)
  - don't hedge retried requests, for simplicity
- need success/fail metrics

retries:

- each strategy handles own counter & sleep
- need metrics for each retry (histo for retry count? i.e. retry1, retry2, retryX...)
- could use Bryan's idea for


```go
type CongestionController interface {
	// extend the client.ObjectClient interface
	client.ObjectClient
	
    RetryStrategy() *RetryStrategy
    HedgingStrategy() *HedgingStrategy
}

type AIMDCongestionController struct {
	inner client.ObjectClient
}

// implement other methods

func (a *AIMDCongestionController) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
    hedging := a.HedgingStrategy()
	retry := a.RetryStrategy()
	
    client := http.DefaultClient
    defaultClient := http.DefaultClient
	
	if hedging != nil {
	    client, err := hedging.HTTPClient(cfg)
		if err != nil {
		    // don't hedge, log error	
        }
    }
	
	// add new interface method; if store doesn't support it, log & do nothing
	// hedging lib hooks into transport, and handles everything
	a.inner.SetHTTPClient(client)
	
	return retry.Do(func(inRetry bool) (io.ReadCloser, int64, error) {
		if inRetry {
		    // disable hedging...?	
            a.inner.SetHTTPClient(defaultClient) // ???
        }
		
        return a.inner.GetObject(ctx, objectKey)
    })

}

type RetryStrategy interface {
	// increments retry counter
	// if retryAgain != true, return actual return variables from GetObject
	Do(func(bool) (io.ReadCloser, int64, error)) (io.ReadCloser, int64, error)
}

type naiveRetryStrategy struct {
	max int
}

func newNaiveRetryStrategy() *naiveRetryStrategy {
	return &naiveRetryStrategy{
	    max: 3,	// 0 = no retries, but 1 initial request
    }
}

func (n *naiveRetryStrategy) Do(fn func(bool) (io.ReadCloser, int64, error)) (io.ReadCloser, int64, error) {
	for i := 0; i <= n.max; i++ {
      r, sz, err := fn(i > 0)
	  
      if !a.inner.IsRetryableError(err) {
        return r, sz, err
      }
	  
	  // metrics increment...
    }
	
	return nil, 0, errors.New("retries exceeded")
}

type HedgingStrategy interface {
	HTTPClient(cfg hedging.Config) (*http.Client, error)
}
```