import {IChannel} from "~/types/rss/podcast/IChannel";

export interface IRss {
    channel: IChannel
    "@_version": string
    "@_xmlns:itunes": string
    "@_xmlns:googleplay": string
    "@_xmlns:atom": string
    "@_xmlns:media": string
    "@_xmlns:content": string
}