package stripe

import sgo "github.com/stripe/stripe-go/v86"

// Re-exported stripe-go types. Aliasing them here means callers can depend on a
// single import (this package) for both the high level helpers and the rich
// resource structs returned by the Stripe API.
type (
	// Account is a (connected) Stripe account.
	Account = sgo.Account
	// AccountLink is a single-use onboarding/update URL for a connected account.
	AccountLink = sgo.AccountLink
	// Customer is a Stripe customer.
	Customer = sgo.Customer
	// Product is a Stripe product.
	Product = sgo.Product
	// Price is a Stripe price (one-off or recurring).
	Price = sgo.Price
	// Subscription is a recurring billing relationship with a customer.
	Subscription = sgo.Subscription
	// CheckoutSession is a hosted Checkout payment page.
	CheckoutSession = sgo.CheckoutSession
	// PaymentIntent represents an intent to collect a one-off payment.
	PaymentIntent = sgo.PaymentIntent
	// Invoice is a Stripe invoice.
	Invoice = sgo.Invoice
	// Coupon is a discount that can be applied to subscriptions/invoices.
	Coupon = sgo.Coupon
	// TaxRate is a tax rate that can be applied to invoices and checkout.
	TaxRate = sgo.TaxRate
	// BillingMeter aggregates usage events for metered billing.
	BillingMeter = sgo.BillingMeter
	// BillingMeterEvent is a single reported usage event.
	BillingMeterEvent = sgo.BillingMeterEvent
	// BillingCreditGrant is a (possibly expiring) credit balance for a customer.
	BillingCreditGrant = sgo.BillingCreditGrant
	// Refund is a full or partial reversal of a captured charge.
	Refund = sgo.Refund
	// WebhookEndpoint is a registered webhook destination.
	WebhookEndpoint = sgo.WebhookEndpoint
	// Event is a webhook event delivered by Stripe.
	Event = sgo.Event
	// OAuthToken is the result of a Connect OAuth token exchange. Its
	// AccessToken can be used directly as an API key (see NewFromOAuthToken).
	OAuthToken = sgo.OAuthToken
	// EventType identifies the kind of a webhook Event (e.g. "invoice.paid").
	EventType = sgo.EventType
	// BillingPortalSession is a single-use link to the hosted customer portal.
	BillingPortalSession = sgo.BillingPortalSession
	// BillingPortalConfiguration controls what customers can do in the portal.
	BillingPortalConfiguration = sgo.BillingPortalConfiguration
	// LoginLink is a single-use link to a connected account's Express dashboard.
	LoginLink = sgo.LoginLink
	// PaymentMethod is a saved card (or other payment instrument).
	PaymentMethod = sgo.PaymentMethod
	// LineItem is a single line of a Checkout session or invoice.
	LineItem = sgo.LineItem
	// Charge is the low level record of a captured (or failed) payment.
	Charge = sgo.Charge
	// SubscriptionStatus is the lifecycle state of a Subscription.
	SubscriptionStatus = sgo.SubscriptionStatus
)

// Subscription statuses, re-exported for switch statements in webhook handlers.
const (
	SubscriptionActive            = sgo.SubscriptionStatusActive
	SubscriptionTrialing          = sgo.SubscriptionStatusTrialing
	SubscriptionPastDue           = sgo.SubscriptionStatusPastDue
	SubscriptionUnpaid            = sgo.SubscriptionStatusUnpaid
	SubscriptionCanceled          = sgo.SubscriptionStatusCanceled
	SubscriptionIncomplete        = sgo.SubscriptionStatusIncomplete
	SubscriptionIncompleteExpired = sgo.SubscriptionStatusIncompleteExpired
	SubscriptionPaused            = sgo.SubscriptionStatusPaused
)

// A small selection of the most frequently handled webhook event types,
// re-exported so callers can register handlers without importing stripe-go.
// The full list lives in the upstream package as sgo.EventType* constants.
const (
	// Checkout
	EventCheckoutSessionCompleted             = sgo.EventTypeCheckoutSessionCompleted
	EventCheckoutSessionExpired               = sgo.EventTypeCheckoutSessionExpired
	EventCheckoutSessionAsyncPaymentSucceeded = sgo.EventTypeCheckoutSessionAsyncPaymentSucceeded
	EventCheckoutSessionAsyncPaymentFailed    = sgo.EventTypeCheckoutSessionAsyncPaymentFailed

	// Customers
	EventCustomerCreated = sgo.EventTypeCustomerCreated
	EventCustomerUpdated = sgo.EventTypeCustomerUpdated
	EventCustomerDeleted = sgo.EventTypeCustomerDeleted

	// Subscriptions
	EventCustomerSubscriptionCreated      = sgo.EventTypeCustomerSubscriptionCreated
	EventCustomerSubscriptionUpdated      = sgo.EventTypeCustomerSubscriptionUpdated
	EventCustomerSubscriptionDeleted      = sgo.EventTypeCustomerSubscriptionDeleted
	EventCustomerSubscriptionTrialWillEnd = sgo.EventTypeCustomerSubscriptionTrialWillEnd
	EventCustomerSubscriptionPaused       = sgo.EventTypeCustomerSubscriptionPaused
	EventCustomerSubscriptionResumed      = sgo.EventTypeCustomerSubscriptionResumed

	// Invoices
	EventInvoicePaid             = sgo.EventTypeInvoicePaid
	EventInvoicePaymentSucceeded = sgo.EventTypeInvoicePaymentSucceeded
	EventInvoicePaymentFailed    = sgo.EventTypeInvoicePaymentFailed
	EventInvoiceUpcoming         = sgo.EventTypeInvoiceUpcoming

	// Payments and refunds
	EventPaymentIntentSucceeded     = sgo.EventTypePaymentIntentSucceeded
	EventPaymentIntentPaymentFailed = sgo.EventTypePaymentIntentPaymentFailed
	EventChargeRefunded             = sgo.EventTypeChargeRefunded
	EventRefundCreated              = sgo.EventTypeRefundCreated
	EventRefundUpdated              = sgo.EventTypeRefundUpdated

	// Connect
	EventAccountUpdated                 = sgo.EventTypeAccountUpdated
	EventAccountApplicationDeauthorized = sgo.EventTypeAccountApplicationDeauthorized
)
