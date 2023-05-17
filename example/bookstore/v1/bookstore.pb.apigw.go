// Code generated by protoc-gen-apigw 0.1.0 from bookstore/v1/bookstore.proto. DO NOT EDIT
package v1

import (
	apigw_v1 "github.com/ductone/protoc-gen-apigw/apigw/v1"
)

func RegisterGatewayBookstoreServiceServer(s apigw_v1.ServiceRegistrar, srv BookstoreServiceServer) {
	s.RegisterService(&apigw_desc_BookstoreServiceServer, srv)
}

var apigw_desc_BookstoreServiceServer = apigw_v1.ServiceDesc{
	Name:        "bookstore.v1.BookstoreService",
	HandlerType: (*BookstoreServiceServer)(nil),
	Methods: []apigw_v1.MethodDesc{
		{
			Name:  ".bookstore.v1.BookstoreService.ListShelves",
			Route: "",
		},
		{
			Name:  ".bookstore.v1.BookstoreService.CreateShelf",
			Route: "",
		},
		{
			Name:  ".bookstore.v1.BookstoreService.DeleteShelf",
			Route: "",
		},
		{
			Name:  ".bookstore.v1.BookstoreService.CreateBook",
			Route: "",
		},
		{
			Name:  ".bookstore.v1.BookstoreService.GetBook",
			Route: "",
		},
		{
			Name:  ".bookstore.v1.BookstoreService.DeleteBook",
			Route: "",
		},
		{
			Name:  ".bookstore.v1.BookstoreService.UpdateBook",
			Route: "",
		},
		{
			Name:  ".bookstore.v1.BookstoreService.GetAuthor",
			Route: "",
		},
	},
}
