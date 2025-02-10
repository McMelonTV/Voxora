import {
    DndProvider,
    type ObjectWithId,
    Draggable,
    DraggableGrid,
    DraggableGridProps,
} from "@mgcrea/react-native-dnd";
import { StyleSheet, Text, View } from "react-native";

const GRID_SIZE = 3;
const items: string[] = ["1", "2", "3", "4", "5", "6", "7", "8", "9"];
const data = items.map((letter, index) => ({
    id: `${index}-${letter}`,
    value: letter,
})) satisfies ObjectWithId[];

export default function PodcastGrid() {
    const onGridOrderChange: DraggableGridProps["onOrderChange"] = (value) => {
        console.log("onGridOrderChange", value);
    };

    return (
        <View style={styles.container}>
            <Text style={styles.title}>DraggableGrid{"\n"}Example</Text>
            <DndProvider>
                <DraggableGrid direction="row" size={GRID_SIZE} style={styles.grid} onOrderChange={onGridOrderChange}>
                    {data.map((item) => (
                        <Draggable key={item.id} id={item.id} style={styles.draggable}>
                            <Text style={styles.text}>{item.value}</Text>
                        </Draggable>
                    ))}
                </DraggableGrid>
            </DndProvider>
        </View>
    );
};

const styles = StyleSheet.create({
    container: {
        flex: 1,
        justifyContent: "center",
        alignItems: "center",
        backgroundColor: "#F5FCFF",
    },
    title: {
        fontSize: 20,
        textAlign: "center",
        margin: 10,
    },
    grid: {
        flex: 1,
        flexDirection: "row",
        flexWrap: "wrap",
        justifyContent: "center",
        alignItems: "center",
    },
    draggable: {
        width: 80,
        height: 80,
        margin: 10,
        backgroundColor: "#F5FCFF",
        justifyContent: "center",
        alignItems: "center",
    },
    text: {
        fontSize: 20,
        textAlign: "center",
    },
});