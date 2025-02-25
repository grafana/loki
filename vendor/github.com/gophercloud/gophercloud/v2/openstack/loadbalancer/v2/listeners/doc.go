/*
Package listeners provides information and interaction with Listeners of the
LBaaS v2 extension for the OpenStack Networking service.

Example to List Listeners

	listOpts := listeners.ListOpts{
		LoadbalancerID : "ca430f80-1737-4712-8dc6-3f640d55594b",
	}

	allPages, err := listeners.List(networkClient, listOpts).AllPages(context.TODO())
	if err != nil {
		panic(err)
	}

	allListeners, err := listeners.ExtractListeners(allPages)
	if err != nil {
		panic(err)
	}

	for _, listener := range allListeners {
		fmt.Printf("%+v\n", listener)
	}

Example to Create a Listener

	createOpts := listeners.CreateOpts{
		Protocol:               "TCP",
		Name:                   "db",
		LoadbalancerID:         "79e05663-7f03-45d2-a092-8b94062f22ab",
		AdminStateUp:           gophercloud.Enabled,
		DefaultPoolID:          "41efe233-7591-43c5-9cf7-923964759f9e",
		ProtocolPort:           3306,
		Tags:                   []string{"test", "stage"},
	}

	listener, err := listeners.Create(context.TODO(), networkClient, createOpts).Extract()
	if err != nil {
		panic(err)
	}

Example to Update a Listener

	listenerID := "d67d56a6-4a86-4688-a282-f46444705c64"

	i1001 := 1001
	i181000 := 181000
	newTags := []string{"prod"}
	updateOpts := listeners.UpdateOpts{
		ConnLimit: &i1001,
		TimeoutClientData: &i181000,
		TimeoutMemberData: &i181000,
		Tags: &newTags,
	}

	listener, err := listeners.Update(context.TODO(), networkClient, listenerID, updateOpts).Extract()
	if err != nil {
		panic(err)
	}

Example to Delete a Listener

	listenerID := "d67d56a6-4a86-4688-a282-f46444705c64"
	err := listeners.Delete(context.TODO(), networkClient, listenerID).ExtractErr()
	if err != nil {
		panic(err)
	}

Example to Get the Statistics of a Listener

	listenerID := "d67d56a6-4a86-4688-a282-f46444705c64"
	stats, err := listeners.GetStats(context.TODO(), networkClient, listenerID).Extract()
	if err != nil {
		panic(err)
	}
*/
package listeners
