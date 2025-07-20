/*
Package loadbalancers provides information and interaction with Load Balancers
of the LBaaS v2 extension for the OpenStack Networking service.

Example to List Load Balancers

	listOpts := loadbalancers.ListOpts{
		Provider: "haproxy",
	}

	allPages, err := loadbalancers.List(networkClient, listOpts).AllPages(context.TODO())
	if err != nil {
		panic(err)
	}

	allLoadbalancers, err := loadbalancers.ExtractLoadBalancers(allPages)
	if err != nil {
		panic(err)
	}

	for _, lb := range allLoadbalancers {
		fmt.Printf("%+v\n", lb)
	}

Example to Create a Load Balancer

	createOpts := loadbalancers.CreateOpts{
		Name:         "db_lb",
		AdminStateUp: gophercloud.Enabled,
		VipSubnetID:  "9cedb85d-0759-4898-8a4b-fa5a5ea10086",
		VipAddress:   "10.30.176.48",
		FlavorID:     "60df399a-ee85-11e9-81b4-2a2ae2dbcce4",
		Provider:     "haproxy",
		Tags:         []string{"test", "stage"},
	}

	lb, err := loadbalancers.Create(context.TODO(), networkClient, createOpts).Extract()
	if err != nil {
		panic(err)
	}

Example to Create a fully populated Load Balancer

	createOpts := loadbalancers.CreateOpts{
		Name:         "db_lb",
		AdminStateUp: gophercloud.Enabled,
		VipSubnetID:  "9cedb85d-0759-4898-8a4b-fa5a5ea10086",
		VipAddress:   "10.30.176.48",
		FlavorID:     "60df399a-ee85-11e9-81b4-2a2ae2dbcce4",
		Provider:     "haproxy",
		Tags:         []string{"test", "stage"},
		Listeners: []listeners.CreateOpts{{
			Protocol:     "HTTP",
			ProtocolPort: 8080,
			Name:         "redirect_listener",
			L7Policies: []l7policies.CreateOpts{{
				Name:        "redirect-example.com",
				Action:      l7policies.ActionRedirectToURL,
				RedirectURL: "http://www.example.com",
				Rules: []l7policies.CreateRuleOpts{{
					RuleType:    l7policies.TypePath,
					CompareType: l7policies.CompareTypeRegex,
					Value:       "/images*",
				}},
			}},
			DefaultPool: &pools.CreateOpts{
				LBMethod: pools.LBMethodRoundRobin,
				Protocol: "HTTP",
				Name:     "example pool",
				Members: []pools.BatchUpdateMemberOpts{{
					Address:      "192.0.2.51",
					ProtocolPort: 80,
				},},
				Monitor: &monitors.CreateOpts{
					Name:           "db",
					Type:           "HTTP",
					Delay:          3,
					MaxRetries:     2,
					Timeout:        1,
				},
			},
		}},
	}

	lb, err := loadbalancers.Create(context.TODO(), networkClient, createOpts).Extract()
	if err != nil {
		panic(err)
	}

Example to Update a Load Balancer

	lbID := "d67d56a6-4a86-4688-a282-f46444705c64"
	name := "new-name"
	updateOpts := loadbalancers.UpdateOpts{
		Name: &name,
	}
	lb, err := loadbalancers.Update(context.TODO(), networkClient, lbID, updateOpts).Extract()
	if err != nil {
		panic(err)
	}

Example to Delete a Load Balancers

	deleteOpts := loadbalancers.DeleteOpts{
		Cascade: true,
	}

	lbID := "d67d56a6-4a86-4688-a282-f46444705c64"

	err := loadbalancers.Delete(context.TODO(), networkClient, lbID, deleteOpts).ExtractErr()
	if err != nil {
		panic(err)
	}

Example to Get the Status of a Load Balancer

	lbID := "d67d56a6-4a86-4688-a282-f46444705c64"
	status, err := loadbalancers.GetStatuses(context.TODO(), networkClient, LBID).Extract()
	if err != nil {
		panic(err)
	}

Example to Get the Statistics of a Load Balancer

	lbID := "d67d56a6-4a86-4688-a282-f46444705c64"
	stats, err := loadbalancers.GetStats(context.TODO(), networkClient, LBID).Extract()
	if err != nil {
		panic(err)
	}

Example to Failover a Load Balancers

	lbID := "d67d56a6-4a86-4688-a282-f46444705c64"

	err := loadbalancers.Failover(context.TODO(), networkClient, lbID).ExtractErr()
	if err != nil {
		panic(err)
	}
*/
package loadbalancers
