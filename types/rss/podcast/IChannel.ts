import {IAtomLink} from "~/types/rss/xml/common/IAtomLink";
import {IImage} from "~/types/rss/xml/common/IImage";
import {IItunesOwner} from "~/types/rss/podcast/itunes/IItunesOwner";
import {IItunesImage} from "~/types/rss/podcast/itunes/IItunesImage";
import {IItunesCategory} from "~/types/rss/podcast/itunes/IItunesCategory";
import {IItem} from "~/types/rss/podcast/IItem";

export interface IChannel {
    "atom:link": IAtomLink
    title: string
    link: string
    language: string
    copyright: string
    description: string
    image: IImage
    "itunes:explicit": string
    "itunes:type": string
    "itunes:subtitle": string
    "itunes:author": string
    "itunes:summary": string
    "content:encoded": string
    "itunes:owner": IItunesOwner
    "itunes:image": IItunesImage
    "itunes:category": IItunesCategory[]
    item: IItem[]
}