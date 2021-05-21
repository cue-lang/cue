#Foo: string

#LoadBalancerSettings: {
	{} | {
		consistentHash: #ConsistentHashLB
		b:              #Foo
	}
	#ConsistentHashLB: {} | {
		httpHeaderName: string
	}
}
