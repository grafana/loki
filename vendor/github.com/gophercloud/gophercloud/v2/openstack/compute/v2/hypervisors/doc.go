/*
Package hypervisors returns details about list of hypervisors, shows details for a hypervisor
and shows summary statistics for all hypervisors over all compute nodes in the OpenStack cloud.

Example of Show Hypervisor Details

	hypervisorID := "42"
	hypervisor, err := hypervisors.Get(context.TODO(), computeClient, hypervisorID).Extract()
	if err != nil {
		panic(err)
	}

	fmt.Printf("%+v\n", hypervisor)

Example of Show Hypervisor Details when using Compute API microversion greater than 2.53

	computeClient.Microversion = "2.53"

	hypervisorID := "c48f6247-abe4-4a24-824e-ea39e108874f"
	hypervisor, err := hypervisors.Get(context.TODO(), computeClient, hypervisorID).Extract()
	if err != nil {
		panic(err)
	}

	fmt.Printf("%+v\n", hypervisor)

Example of Retrieving Details of All Hypervisors

	allPages, err := hypervisors.List(computeClient, nil).AllPages(context.TODO())
	if err != nil {
		panic(err)
	}

	allHypervisors, err := hypervisors.ExtractHypervisors(allPages)
	if err != nil {
		panic(err)
	}

	for _, hypervisor := range allHypervisors {
		fmt.Printf("%+v\n", hypervisor)
	}

Example of Retrieving Details of All Hypervisors when using Compute API microversion 2.33 or greater.

	computeClient.Microversion = "2.53"

	iTrue := true
	listOpts := hypervisors.ListOpts{
		WithServers: &true,
	}

	allPages, err := hypervisors.List(computeClient, listOpts).AllPages(context.TODO())
	if err != nil {
		panic(err)
	}

	allHypervisors, err := hypervisors.ExtractHypervisors(allPages)
	if err != nil {
		panic(err)
	}

	for _, hypervisor := range allHypervisors {
		fmt.Printf("%+v\n", hypervisor)
	}

Example of Show Hypervisors Statistics

	hypervisorsStatistics, err := hypervisors.GetStatistics(context.TODO(), computeClient).Extract()
	if err != nil {
		panic(err)
	}

	fmt.Printf("%+v\n", hypervisorsStatistics)

Example of Show Hypervisor Uptime

	hypervisorID := "42"
	hypervisorUptime, err := hypervisors.GetUptime(context.TODO(), computeClient, hypervisorID).Extract()
	if err != nil {
		panic(err)
	}

	fmt.Printf("%+v\n", hypervisorUptime)

Example of Show Hypervisor Uptime with Compute API microversion greater than 2.53

	computeClient.Microversion = "2.53"

	hypervisorID := "c48f6247-abe4-4a24-824e-ea39e108874f"
	hypervisorUptime, err := hypervisors.GetUptime(context.TODO(), computeClient, hypervisorID).Extract()
	if err != nil {
		panic(err)
	}

	fmt.Printf("%+v\n", hypervisorUptime)
*/
package hypervisors
