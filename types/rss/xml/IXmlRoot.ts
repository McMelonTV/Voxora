import {IXml} from "~/types/rss/xml/IXml";
import {IRss} from "~/types/rss/IRss";

export interface IXmlRoot {
    "?xml": IXml
    rss: IRss
}