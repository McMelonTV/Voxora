import {IGuid} from "~/types/rss/xml/common/IGuid";
import {IEnclosure} from "~/types/rss/xml/common/IEnclosure";
import {IItunesImage} from "~/types/rss/podcast/itunes/IItunesImage";

export interface IItem {
    title: string
    link?: string
    description: string
    pubDate: string
    "itunes:title"?: string
    "itunes:episodeType": string
    "itunes:author": string
    "itunes:image"?: IItunesImage
    "itunes:subtitle": string
    "itunes:summary": string
    "content:encoded": string
    "itunes:duration": number
    guid: IGuid
    enclosure: IEnclosure
    "itunes:season"?: number
    "itunes:episode"?: number
    "itunes:explicit"?: string
}