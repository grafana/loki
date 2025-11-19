package dynamodb

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/internal/awsutil"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// BatchGetItemPaginatorOptions is the paginator options for BatchGetItem
type BatchGetItemPaginatorOptions struct {
	// Set to true if pagination should stop if the service returns a pagination token
	// that matches the most recent token provided to the service.
	StopOnDuplicateToken bool
}

// BatchGetItemPaginator is a paginator for BatchGetItem
type BatchGetItemPaginator struct {
	options      BatchGetItemPaginatorOptions
	client       BatchGetItemAPIClient
	params       *BatchGetItemInput
	firstPage    bool
	requestItems map[string]types.KeysAndAttributes
	isTruncated  bool
}

// BatchGetItemAPIClient is a client that implements the BatchGetItem operation.
type BatchGetItemAPIClient interface {
	BatchGetItem(context.Context, *BatchGetItemInput, ...func(*Options)) (*BatchGetItemOutput, error)
}

// NewBatchGetItemPaginator returns a new BatchGetItemPaginator
func NewBatchGetItemPaginator(client BatchGetItemAPIClient, params *BatchGetItemInput, optFns ...func(*BatchGetItemPaginatorOptions)) *BatchGetItemPaginator {
	if params == nil {
		params = &BatchGetItemInput{}
	}

	options := BatchGetItemPaginatorOptions{}

	for _, fn := range optFns {
		fn(&options)
	}

	return &BatchGetItemPaginator{
		options:      options,
		client:       client,
		params:       params,
		firstPage:    true,
		requestItems: params.RequestItems,
	}
}

// HasMorePages returns a boolean indicating whether more pages are available
func (p *BatchGetItemPaginator) HasMorePages() bool {
	return p.firstPage || p.isTruncated
}

// NextPage retrieves the next BatchGetItem page.
func (p *BatchGetItemPaginator) NextPage(ctx context.Context, optFns ...func(*Options)) (*BatchGetItemOutput, error) {
	if !p.HasMorePages() {
		return nil, fmt.Errorf("no more pages available")
	}

	params := *p.params
	params.RequestItems = p.requestItems

	result, err := p.client.BatchGetItem(ctx, &params, optFns...)
	if err != nil {
		return nil, err
	}
	p.firstPage = false

	prevToken := p.requestItems
	p.isTruncated = len(result.UnprocessedKeys) != 0
	p.requestItems = nil
	if p.isTruncated {
		p.requestItems = result.UnprocessedKeys
	}

	if p.options.StopOnDuplicateToken &&
		prevToken != nil &&
		p.requestItems != nil &&
		awsutil.DeepEqual(prevToken, p.requestItems) {
		p.isTruncated = false
	}

	return result, nil
}
