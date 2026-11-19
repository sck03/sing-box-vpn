package manage

import "github.com/sck03/sing-box-vpn/internal/servicecatalog"

var serviceDefinitions = servicecatalog.Definitions()
var platformServices = servicecatalog.Names()
var serviceDetails = servicecatalog.Lookup
var scopeService = servicecatalog.RuntimeService
