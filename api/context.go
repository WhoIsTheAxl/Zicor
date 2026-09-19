package api

import "github.com/WhoIsTheAxl/Zicor/api/minecraft"

type ModContext interface {
    Client() minecraft.Client
}