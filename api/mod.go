package api

type Mod interface {
    Init(ctx *ModContext)
}