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

	// PaymentMethod optionally attaches a payment method ("pm_...") and makes
	// it the customer's default, so Subscribe and ChargeCustomer can bill it
	// without a Checkout page. In test mode use a test token such as
	// "pm_card_visa".
	PaymentMethod string
}

// CreateCustomer creates a new Stripe customer. The customer id returned is what
// you pass to Subscribe, Checkout and ChargeWithFee.
//
// Store the returned Customer.ID next to your own user record; it is the link
// between your user and everything they buy.
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
	cus, err := c.api.V1Customers.Create(ctx, params)
	if err != nil || p.PaymentMethod == "" {
		return cus, err
	}
	if _, err := c.AttachPaymentMethod(ctx, cus.ID, p.PaymentMethod); err != nil {
		return cus, err
	}
	return c.GetCustomer(ctx, cus.ID)
}

// AttachPaymentMethod attaches a payment method ("pm_...", or a test token such
// as "pm_card_visa") to a customer and makes it their default for invoices,
// subscriptions and ChargeCustomer. It returns the attached payment method.
func (c *Client) AttachPaymentMethod(ctx context.Context, customerID, paymentMethodID string) (*PaymentMethod, error) {
	if customerID == "" || paymentMethodID == "" {
		return nil, fmt.Errorf("stripe: AttachPaymentMethod requires a customerID and paymentMethodID")
	}
	attach := &sgo.PaymentMethodAttachParams{Customer: String(customerID)}
	c.prep(&attach.Params)
	pm, err := c.api.V1PaymentMethods.Attach(ctx, paymentMethodID, attach)
	if err != nil {
		return nil, err
	}
	upd := &sgo.CustomerUpdateParams{
		InvoiceSettings: &sgo.CustomerUpdateInvoiceSettingsParams{DefaultPaymentMethod: String(pm.ID)},
	}
	c.prep(&upd.Params)
	if _, err := c.api.V1Customers.Update(ctx, customerID, upd); err != nil {
		return nil, err
	}
	return pm, nil
}

// FindCustomerByEmail returns the most recently created customer with the given
// email. found is false when there is none. Emails are not unique in Stripe,
// so prefer storing the customer id on your user record and use this only as a
// fallback (e.g. to avoid creating duplicates for returning guests).
func (c *Client) FindCustomerByEmail(ctx context.Context, email string) (cus *Customer, found bool, err error) {
	if email == "" {
		return nil, false, fmt.Errorf("stripe: FindCustomerByEmail requires an email")
	}
	params := &sgo.CustomerListParams{Email: String(email)}
	params.Limit = Int64(1)
	c.prepList(&params.ListParams)
	for cus, err := range c.api.V1Customers.List(ctx, params).All(ctx) {
		if err != nil {
			return nil, false, err
		}
		return cus, true, nil
	}
	return nil, false, nil
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
