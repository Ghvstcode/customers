package customers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	moovhttp "github.com/moov-io/base/http"
	"github.com/moov-io/base/log"

	"github.com/moov-io/customers/pkg/route"
)

type AddressType int

const (
	AddressType_Primary   AddressType = 1
	AddressType_Secondary AddressType = 2
)

func (a AddressType) Common() string {
	switch a {
	case AddressType_Primary:
		return "primary"
	case AddressType_Secondary:
		return "secondary"
	}
	return ""
}

func (a AddressType) String() string {
	return fmt.Sprintf("'%s'", a.Common())
}

func addressTypeToModel(v string) (AddressType, error) {
	m := map[string]AddressType{
		"primary":   AddressType_Primary,
		"secondary": AddressType_Secondary,
	}

	addrType, ok := m[v]
	if !ok {
		return 0, fmt.Errorf("unknown addresss type: %s", v)
	}
	return addrType, nil
}

var (
	ErrAddressTypeDuplicate = errors.New("customer already has an address with type 'primary'")
)

func AddCustomerAddressRoutes(logger log.Logger, r *mux.Router, repo CustomerRepository) {
	logger = logger.Set("package", "customers")

	r.Methods("POST").Path("/customers/{customerID}/addresses").HandlerFunc(createCustomerAddress(logger, repo))
	r.Methods("PUT").Path("/customers/{customerID}/addresses/{addressID}").HandlerFunc(updateCustomerAddress(logger, repo))
	r.Methods("DELETE").Path("/customers/{customerID}/addresses/{addressID}").HandlerFunc(deleteCustomerAddress(logger, repo))
}

func createCustomerAddress(logger log.Logger, repo CustomerRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID, requestID := route.GetCustomerID(w, r), moovhttp.GetRequestID(r)
		if customerID == "" {
			return
		}

		organization := route.GetOrganization(w, r)
		if organization == "" {
			return
		}

		var reqAddr address
		if err := json.NewDecoder(r.Body).Decode(&reqAddr); err != nil {
			moovhttp.Problem(w, err)
			return
		}

		cust, err := repo.GetCustomer(customerID, organization)
		if err != nil {
			moovhttp.Problem(w, err)
			return
		}
		if cust == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// todo: vince-10/12/2020: we need to perform this conversion for validation til we develop a clean separation layer between client and model structs
		var addrs []address
		for _, addr := range cust.Addresses {
			addrs = append(addrs, address{
				Type:       strings.ToLower(addr.Type),
				Address1:   addr.Address1,
				Address2:   addr.Address2,
				City:       addr.City,
				State:      addr.State,
				PostalCode: addr.PostalCode,
				Country:    addr.Country,
			})
		}
		if err := validateAddresses(append(addrs, reqAddr)); err != nil {
			moovhttp.Problem(w, err)
			return
		}

		if err := repo.addCustomerAddress(customerID, reqAddr); err != nil {
			moovhttp.Problem(w, err)
			return
		}

		logger.Logf("added address for customer=%s", customerID)
		respondWithCustomer(logger, w, customerID, organization, requestID, repo)
	}
}

func updateCustomerAddress(logger log.Logger, repo CustomerRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w = route.Responder(logger, w, r)

		customerID, addressId := route.GetCustomerID(w, r), getAddressId(w, r)
		if customerID == "" || addressId == "" {
			return
		}

		requestID := moovhttp.GetRequestID(r)
		organization := route.GetOrganization(w, r)
		if organization == "" {
			return
		}

		var req updateCustomerAddressRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			moovhttp.Problem(w, err)
			return
		}

		if err := req.validate(); err != nil {
			moovhttp.Problem(w, err)
			return
		}

		if req.Type == AddressType_Primary.Common() {
			cust, err := repo.GetCustomer(customerID, organization)
			if err != nil {
				moovhttp.Problem(w, err)
				return
			}

			for _, addr := range cust.Addresses {
				if addr.Type == "primary" && addr.AddressID != addressId {
					moovhttp.Problem(w, ErrAddressTypeDuplicate)
					return
				}
			}
		}

		if err := repo.updateCustomerAddress(customerID, addressId, req); err != nil {
			logger.LogErrorf("error updating customer's address: customer=%s address=%s: %v", customerID, addressId, err)
			moovhttp.Problem(w, err)
			return
		}

		logger.Logf("updating address=%s for customer=%s", addressId, customerID)

		respondWithCustomer(logger, w, customerID, organization, requestID, repo)
	}
}

func deleteCustomerAddress(logger log.Logger, repo CustomerRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w = route.Responder(logger, w, r)

		customerID, addressId := route.GetCustomerID(w, r), getAddressId(w, r)
		if customerID == "" || addressId == "" {
			return
		}

		err := repo.deleteCustomerAddress(customerID, addressId)
		if err != nil {
			logger.LogErrorf("error deleting customer's address: customer=%s address=%s: %v", customerID, addressId, err)
			moovhttp.Problem(w, err)
			return
		}

		logger.Logf("successfully deleted address=%s for customer=%s", addressId, customerID)

		w.WriteHeader(http.StatusNoContent)
	}
}

func getAddressId(w http.ResponseWriter, r *http.Request) string {
	varName := "addressID"
	v, ok := mux.Vars(r)[varName]
	if !ok || v == "" {
		moovhttp.Problem(w, fmt.Errorf("path variable %s not found in url", varName))
		return ""
	}
	return v
}

type updateCustomerAddressRequest struct {
	address   `json:",inline"`
	Validated bool `json:"validated"`
}
