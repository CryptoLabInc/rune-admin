package server

// runespace integration: the former EnsureVault (enVector cloud key
// registration + index creation) is obsolete. Under the runespace model the
// vault registers its eval key directly with the runespace engine via
// crypto.OpenEngine (RegisterKeys) at daemon startup, so there is no separate
// cloud-setup step here.
