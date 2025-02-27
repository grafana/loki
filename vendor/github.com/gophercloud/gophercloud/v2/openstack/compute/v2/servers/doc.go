/*
Package servers provides information and interaction with the server API
resource in the OpenStack Compute service.

A server is a virtual machine instance in the compute system. In order for
one to be provisioned, a valid flavor and image are required.

Example to List Servers

	listOpts := servers.ListOpts{
		AllTenants: true,
	}

	allPages, err := servers.ListSimple(computeClient, listOpts).AllPages(context.TODO())
	if err != nil {
		panic(err)
	}

	allServers, err := servers.ExtractServers(allPages)
	if err != nil {
		panic(err)
	}

	for _, server := range allServers {
		fmt.Printf("%+v\n", server)
	}

Example to List Detail Servers

	listOpts := servers.ListOpts{
		AllTenants: true,
	}

	allPages, err := servers.List(computeClient, listOpts).AllPages(context.TODO())
	if err != nil {
		panic(err)
	}

	allServers, err := servers.ExtractServers(allPages)
	if err != nil {
		panic(err)
	}

	for _, server := range allServers {
		fmt.Printf("%+v\n", server)
	}

Example to Create a Server

	createOpts := servers.CreateOpts{
		Name:      "server_name",
		ImageRef:  "image-uuid",
		FlavorRef: "flavor-uuid",
	}

	server, err := servers.Create(context.TODO(), computeClient, createOpts, nil).Extract()
	if err != nil {
		panic(err)
	}

Example to Add a Server to a Server Group

	schedulerHintOpts := servers.SchedulerHintOpts{
		Group: "servergroup-uuid",
	}

	createOpts := servers.CreateOpts{
		Name:      "server_name",
		ImageRef:  "image-uuid",
		FlavorRef: "flavor-uuid",
	}

	server, err := servers.Create(context.TODO(), computeClient, createOpts, schedulerHintOpts).Extract()
	if err != nil {
		panic(err)
	}

Example to Place Server B on a Different Host than Server A

	schedulerHintOpts := servers.SchedulerHintOpts{
		DifferentHost: []string{
			"server-a-uuid",
		}
	}

	createOpts := servers.CreateOpts{
		Name:      "server_b",
		ImageRef:  "image-uuid",
		FlavorRef: "flavor-uuid",
	}

	server, err := servers.Create(context.TODO(), computeClient, createOpts, schedulerHintOpts).Extract()
	if err != nil {
		panic(err)
	}

Example to Place Server B on the Same Host as Server A

	schedulerHintOpts := servers.SchedulerHintOpts{
		SameHost: []string{
			"server-a-uuid",
		}
	}

	createOpts := servers.CreateOpts{
		Name:      "server_b",
		ImageRef:  "image-uuid",
		FlavorRef: "flavor-uuid",
	}

	server, err := servers.Create(context.TODO(), computeClient, createOpts, schedulerHintOpts).Extract()
	if err != nil {
		panic(err)
	}

# Example to Create a Server From an Image

This example will boot a server from an image and use a standard ephemeral
disk as the server's root disk. This is virtually no different than creating
a server without using block device mappings.

	blockDevices := []servers.BlockDevice{
		servers.BlockDevice{
			BootIndex:           0,
			DeleteOnTermination: true,
			DestinationType:     servers.DestinationLocal,
			SourceType:          servers.SourceImage,
			UUID:                "image-uuid",
		},
	}

	createOpts := servers.CreateOpts{
		Name:        "server_name",
		FlavorRef:   "flavor-uuid",
		ImageRef:    "image-uuid",
		BlockDevice: blockDevices,
	}

	server, err := servers.Create(context.TODO(), client, createOpts, nil).Extract()
	if err != nil {
		panic(err)
	}

# Example to Create a Server From a New Volume

This example will create a block storage volume based on the given Image. The
server will use this volume as its root disk.

	blockDevices := []servers.BlockDevice{
		servers.BlockDevice{
			DeleteOnTermination: true,
			DestinationType:     servers.DestinationVolume,
			SourceType:          servers.SourceImage,
			UUID:                "image-uuid",
			VolumeSize:          2,
		},
	}

	createOpts := servers.CreateOpts{
		Name:        "server_name",
		FlavorRef:   "flavor-uuid",
		BlockDevice: blockDevices,
	}

	server, err := servers.Create(context.TODO(), client, createOpts, nil).Extract()
	if err != nil {
		panic(err)
	}

# Example to Create a Server From an Existing Volume

This example will create a server with an existing volume as its root disk.

	blockDevices := []servers.BlockDevice{
		servers.BlockDevice{
			DeleteOnTermination: true,
			DestinationType:     servers.DestinationVolume,
			SourceType:          servers.SourceVolume,
			UUID:                "volume-uuid",
		},
	}

	createOpts := servers.CreateOpts{
		Name:        "server_name",
		FlavorRef:   "flavor-uuid",
		BlockDevice: blockDevices,
	}

	server, err := servers.Create(context.TODO(), client, createOpts, nil).Extract()
	if err != nil {
		panic(err)
	}

# Example to Create a Server with Multiple Ephemeral Disks

This example will create a server with multiple ephemeral disks. The first
block device will be based off of an existing Image. Each additional
ephemeral disks must have an index of -1.

	blockDevices := []servers.BlockDevice{
		servers.BlockDevice{
			BootIndex:           0,
			DestinationType:     servers.DestinationLocal,
			DeleteOnTermination: true,
			SourceType:          servers.SourceImage,
			UUID:                "image-uuid",
			VolumeSize:          5,
		},
		servers.BlockDevice{
			BootIndex:           -1,
			DestinationType:     servers.DestinationLocal,
			DeleteOnTermination: true,
			GuestFormat:         "ext4",
			SourceType:          servers.SourceBlank,
			VolumeSize:          1,
		},
		servers.BlockDevice{
			BootIndex:           -1,
			DestinationType:     servers.DestinationLocal,
			DeleteOnTermination: true,
			GuestFormat:         "ext4",
			SourceType:          servers.SourceBlank,
			VolumeSize:          1,
		},
	}

	CreateOpts := servers.CreateOpts{
		Name:        "server_name",
		FlavorRef:   "flavor-uuid",
		ImageRef:    "image-uuid",
		BlockDevice: blockDevices,
	}

	server, err := servers.Create(context.TODO(), client, createOpts, nil).Extract()
	if err != nil {
		panic(err)
	}

Example to Delete a Server

	serverID := "d9072956-1560-487c-97f2-18bdf65ec749"
	err := servers.Delete(context.TODO(), computeClient, serverID).ExtractErr()
	if err != nil {
		panic(err)
	}

Example to Force Delete a Server

	serverID := "d9072956-1560-487c-97f2-18bdf65ec749"
	err := servers.ForceDelete(context.TODO(), computeClient, serverID).ExtractErr()
	if err != nil {
		panic(err)
	}

Example to Reboot a Server

	rebootOpts := servers.RebootOpts{
		Type: servers.SoftReboot,
	}

	serverID := "d9072956-1560-487c-97f2-18bdf65ec749"

	err := servers.Reboot(context.TODO(), computeClient, serverID, rebootOpts).ExtractErr()
	if err != nil {
		panic(err)
	}

Example to Rebuild a Server

	rebuildOpts := servers.RebuildOpts{
		Name:    "new_name",
		ImageID: "image-uuid",
	}

	serverID := "d9072956-1560-487c-97f2-18bdf65ec749"

	server, err := servers.Rebuilt(computeClient, serverID, rebuildOpts).Extract()
	if err != nil {
		panic(err)
	}

Example to Resize a Server

	resizeOpts := servers.ResizeOpts{
		FlavorRef: "flavor-uuid",
	}

	serverID := "d9072956-1560-487c-97f2-18bdf65ec749"

	err := servers.Resize(context.TODO(), computeClient, serverID, resizeOpts).ExtractErr()
	if err != nil {
		panic(err)
	}

	err = servers.ConfirmResize(context.TODO(), computeClient, serverID).ExtractErr()
	if err != nil {
		panic(err)
	}

Example to Snapshot a Server

	snapshotOpts := servers.CreateImageOpts{
		Name: "snapshot_name",
	}

	serverID := "d9072956-1560-487c-97f2-18bdf65ec749"

	image, err := servers.CreateImage(context.TODO(), computeClient, serverID, snapshotOpts).ExtractImageID()
	if err != nil {
		panic(err)
	}
*/
package servers
