package stripe

import (
	"context"
	"fmt"

	sgo "github.com/stripe/stripe-go/v86"
)

// CreateCustomerParams describes a new customer to register on the platform (or
// on a connected account when the client is scoped via ForAccount).
type CreateCustomerParams struct {
	Email       string
	Name        string
	Phone       string
	Description string
	Metadata    map[string]string
}

// CreateCustomer creates a new Stripe customer. The customer id returned is what
// you pass to Subscribe, Checkout and ChargeWithFee.
func (c *Client) CreateCustomer(ctx context.Context, p CreateCustomerParams) (*Customer, error) {
	params := &sgo.CustomerCreateParams{}
	if p.Email != "" {
		params.Email = String(p.Email)
	}
	if p.Name != "" {
		params.Name = String(p.Name)
	}
	if p.Phone != "" {
		params.Phone = String(p.Phone)
	}
	if p.Description != "" {
		params.Description = String(p.Description)
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	c.prep(&params.Params)
	return c.api.V1Customers.Create(ctx, params)
}

// GetCustomer retrieves a customer by id.
func (c *Client) GetCustomer(ctx context.Context, customerID string) (*Customer, error) {
	if customerID == "" {
		return nil, fmt.Errorf("stripe: GetCustomer requires a customerID")
	}
	params := &sgo.CustomerRetrieveParams{}
	c.prep(&params.Params)
	return c.api.V1Customers.Retrieve(ctx, customerID, params)
}

// UpdateCustomerParams carries the mutable fields of a customer.
// Empty/zero fields are left untouched.
type UpdateCustomerParams struct {
	Email       string
	Name        string
	Phone       string
	Description string
	Metadata    map[string]string
}

// UpdateCustomer patches an existing customer.
func (c *Client) UpdateCustomer(ctx context.Context, customerID string, p UpdateCustomerParams) (*Customer, error) {
	if customerID == "" {
		return nil, fmt.Errorf("stripe: UpdateCustomer requires a customerID")
	}
	params := &sgo.CustomerUpdateParams{}
	if p.Email != "" {
		params.Email = String(p.Email)
	}
	if p.Name != "" {
		params.Name = String(p.Name)
	}
	if p.Phone != "" {
		params.Phone = String(p.Phone)
	}
	if p.Description != "" {
		params.Description = String(p.Description)
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}
	c.prep(&params.Params)
	return c.api.V1Customers.Update(ctx, customerID, params)
}

// DeleteCustomer permanently deletes a customer and cancels all active
// subscriptions. This cannot be undone.
func (c *Client) DeleteCustomer(ctx context.Context, customerID string) error {
	if customerID == "" {
		return fmt.Errorf("stripe: DeleteCustomer requires a customerID")
	}
	params := &sgo.CustomerDeleteParams{}
	c.prep(&params.Params)
	_, err := c.api.V1Customers.Delete(ctx, customerID, params)
	return err
}
