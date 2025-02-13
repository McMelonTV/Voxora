import {useState} from 'react';
import {
    View,
    StyleSheet,
    Text, Dimensions,
} from 'react-native';
import {DraggableGrid} from "~/components/dnd-grid/src";
import {ScrollView} from "react-native-gesture-handler";

export default function PodcastGrid() {
    const [data, setData] = useState([
        {name: '1', key: 'one'},
        {name: '2', key: 'two'},
        {name: '3', key: 'three'},
        {name: '4', key: 'four'},
        {name: '5', key: 'five'},
        {name: '6', key: 'six'},
        {name: '7', key: 'seven'},
        {name: '8', key: 'eight'},
        {name: '9', key: 'night'},
        {name: '10', key: 'ten'},
        {name: '11', key: 'eleven'},
        {name: '12', key: 'twelve'},
        {name: '13', key: 'thirteen'},
        {name: '14', key: 'fourteen'},
        {name: '15', key: 'fifteen'},
        {name: '16', key: 'sixteen'},
        {name: '17', key: 'seventeen'},
        {name: '18', key: 'eighteen'},
        {name: '19', key: 'nineteen'},
        {name: '20', key: 'twenty'},
    ]);

    const [dragging, setDragging] = useState(false);

    const gap = 20;
    const windowWidth = Dimensions.get('window').width;
    const itemSize = 100;
    const numColumns = Math.floor(windowWidth / (itemSize + gap / 2));

    const render_item = (item: { name: string, key: string }) => (
        <View style={{width: itemSize, height: itemSize, ...styles.item}} key={item.key}>
            <Text style={styles.item_text}>{item.name}</Text>
        </View>
    );

    return (
        <ScrollView style={styles.wrapper} scrollEnabled={!dragging}>
            <DraggableGrid
                numColumns={numColumns}
                renderItem={render_item}
                data={data}
                onItemPress={() => setDragging(true)}
                onDragItemActive={() => setDragging(true)}
                onDragRelease={(newData) => {
                    setData(newData);
                    setDragging(false);
                }}
                delayLongPress={100}
                gap={gap}
            />
        </ScrollView>
    );
}

const styles = StyleSheet.create({
    wrapper: {
        width: '100%',
        height: '100%',
        overflow: 'scroll',
    },
    item: {
        borderRadius: 8,
        backgroundColor: 'red',
        justifyContent: 'center',
        alignItems: 'center',
    },
    item_text: {
        fontSize: 40,
        color: '#FFFFFF',
    },
});

