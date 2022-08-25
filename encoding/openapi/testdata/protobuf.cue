// Note: This example originally derived from a CUE file converted from protobuf.

#HTTPRoute: {
	route: [...#HTTPRouteDestination]
}
#HTTPRouteDestination: {
	headers:     #Headers
}
#Headers: {
	request:  #HeaderOperations
	#HeaderOperations:  {}
}
