import {View, Text, StyleSheet} from 'react-native';
import {useEffect} from "react";
import {XMLParser, XMLValidator} from "fast-xml-parser";
import {IXmlRoot} from "~/types/rss/xml/IXmlRoot";
import PodcastGrid from "~/components/PodcastGrid";

export default function Tab() {
    useEffect(() => {
        (async () => {
            const Parser = new XMLParser({
                ignoreAttributes: false
            });
            const xml = await (await fetch('https://feeds.megaphone.fm/QCEOS5292368649')).text();
            // const xml = await (await fetch('https://gist.githubusercontent.com/rodydavis/d0cb7a53d8deb42e92ae803a9dd48dbc/raw/94c3a5bef2c662122d3663e0c27e7db2d7fe2cd7/podcast_rss_feed.xml')).text();
            const valid = XMLValidator.validate(xml);
            console.log(valid);
            const data: IXmlRoot = Parser.parse(xml);
            // console.log(JSON.stringify(data));
            console.log(data.rss.channel.item[0].enclosure["@_url"]);
        })();
    }, []);

    return (
        <View style={styles.container}>
            <PodcastGrid />
        </View>
    );
}

const styles = StyleSheet.create({
    container: {
        flex: 1,
        justifyContent: 'center',
        alignItems: 'center',
    },
});
